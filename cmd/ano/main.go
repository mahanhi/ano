package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/ano/ano/internal/agent"
	"github.com/ano/ano/internal/chat"
	"github.com/ano/ano/internal/envoyrun"
	"github.com/ano/ano/internal/gate"
	"github.com/ano/ano/internal/lan"
	"github.com/ano/ano/internal/llm"
	"github.com/ano/ano/internal/store"
	"github.com/ano/ano/internal/via"
)

func main() {
	dataDir := os.Getenv("ANO_DATA")
	if dataDir == "" {
		dataDir = "data"
	}
	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		log.Fatal(err)
	}
	st, err := store.Open(filepath.Join(dataDir, "ano.db"))
	if err != nil {
		log.Fatal(err)
	}
	defer st.Close()
	envoy := envoyrun.Start(st, dataDir)
	defer envoy.Stop()
	viaAddr, upstream := lan.ListenAddr()
	path := via.Start(st, dataDir, upstream)
	defer path.Stop()
	gateway := gate.Start(st, dataDir)
	go gateway.Loop()
	go gate.Follow(st, filepath.Join(dataDir, "envoy-access.log"))
	withVia := func(view store.AdminView) store.AdminView {
		status := path.Status()
		view.ViaAddr = viaAddr
		view.ViaPort = via.ProxyPort
		view.ProxyReady = status.Proxy
		view.DNSReady = status.DNS
		view.DNSDetail = status.DNSDetail
		view.SteerOn = status.Steer
		view.SteerDetail = status.SteerDetail
		return view
	}
	reload := func() error {
		if err := envoy.Reload(); err != nil {
			return err
		}
		return gateway.Refresh()
	}
	ag := &agent.Agent{
		Store:     st,
		Model:     llm.Client{Settings: llm.FromEnv()},
		OnEnforce: reload,
	}
	go ag.Loop(context.Background())
	go watchNetwork(st, path)

	talk := &chat.Service{Store: st, Agent: ag, Model: ag.Model, Apply: reload, Gate: gateway}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/health", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"ok": "true", "product": "Ano"})
	})
	mux.HandleFunc("POST /api/chat", func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Text string `json:"text"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			writeErr(w, http.StatusBadRequest, errors.New("send a request"))
			return
		}
		reply, err := talk.Ask(r.Context(), body.Text)
		if err != nil {
			writeErr(w, http.StatusBadGateway, err)
			return
		}
		writeJSON(w, http.StatusOK, reply)
	})
	mux.HandleFunc("GET /api/state", func(w http.ResponseWriter, r *http.Request) {
		speakerOnly, err := st.SpeakerOnly()
		if err != nil {
			writeErr(w, http.StatusInternalServerError, err)
			return
		}
		mode, err := st.Mode()
		if err != nil {
			writeErr(w, http.StatusInternalServerError, err)
			return
		}
		cards, err := st.Cards(speakerOnly && r.URL.Query().Get("all") != "1")
		if err != nil {
			writeErr(w, http.StatusInternalServerError, err)
			return
		}
		devices, err := st.Devices()
		if err != nil {
			writeErr(w, http.StatusInternalServerError, err)
			return
		}
		decisions, err := st.Decisions(40)
		if err != nil {
			writeErr(w, http.StatusInternalServerError, err)
			return
		}
		series, err := st.Series()
		if err != nil {
			writeErr(w, http.StatusInternalServerError, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"settings":  map[string]any{"speakerOnly": speakerOnly, "mode": mode},
			"cards":     cards,
			"devices":   devices,
			"decisions": decisions,
			"series":    series,
			"envoy":     envoy.Status(),
			"agent":     ag.Snapshot(),
			"via":       viaStatus(viaAddr, path),
			"addr":      lan.ScreenAddr(),
		})
	})
	mux.HandleFunc("PUT /api/settings", func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			SpeakerOnly *bool   `json:"speakerOnly"`
			Mode        *string `json:"mode"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			writeErr(w, http.StatusBadRequest, err)
			return
		}
		if body.SpeakerOnly == nil && body.Mode == nil {
			writeErr(w, http.StatusBadRequest, errors.New("send speakerOnly or mode"))
			return
		}
		if body.SpeakerOnly != nil {
			if err := st.SetSpeakerOnly(*body.SpeakerOnly); err != nil {
				writeErr(w, http.StatusInternalServerError, err)
				return
			}
		}
		if body.Mode != nil {
			if err := ag.SetMode(*body.Mode); err != nil {
				writeErr(w, http.StatusBadRequest, err)
				return
			}
		}
		mode, _ := st.Mode()
		only, _ := st.SpeakerOnly()
		writeJSON(w, http.StatusOK, map[string]any{"speakerOnly": only, "mode": mode, "agent": ag.Snapshot()})
	})
	mux.HandleFunc("POST /api/admin/review-schedule", func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			On       *bool   `json:"on"`
			Every    *int    `json:"every"`
			Unit     *string `json:"unit"`
			Hour     *int    `json:"hour"`
			Minute   *int    `json:"minute"`
			Weekday  *int    `json:"weekday"`
			MonthDay *int    `json:"monthDay"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.On == nil || body.Every == nil || body.Unit == nil || body.Hour == nil || body.Minute == nil || body.Weekday == nil || body.MonthDay == nil {
			writeErr(w, http.StatusBadRequest, errors.New("send the schedule"))
			return
		}
		if err := st.SetReviewSchedule(*body.On, *body.Every, *body.Unit, *body.Hour, *body.Minute, *body.Weekday, *body.MonthDay); err != nil {
			writeErr(w, http.StatusBadRequest, err)
			return
		}
		view, err := st.AdminView()
		if err != nil {
			writeErr(w, http.StatusInternalServerError, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"admin": withVia(view), "envoy": envoy.Status()})
	})
	mux.HandleFunc("GET /api/admin/blocks", func(w http.ResponseWriter, r *http.Request) {
		drops, err := st.DropNames()
		if err != nil {
			writeErr(w, http.StatusInternalServerError, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"drops": drops})
	})
	mux.HandleFunc("POST /api/admin/blocks", func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Name string `json:"name"`
			Port int    `json:"port"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			writeErr(w, http.StatusBadRequest, err)
			return
		}
		drops, err := st.AddBlock(body.Name, body.Port)
		if err != nil {
			writeErr(w, http.StatusBadRequest, err)
			return
		}
		if err := reload(); err != nil {
			writeErr(w, http.StatusInternalServerError, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"drops": drops})
	})
	mux.HandleFunc("DELETE /api/admin/blocks/{id}", func(w http.ResponseWriter, r *http.Request) {
		drops, err := st.DeleteBlock(r.PathValue("id"))
		if err != nil {
			writeErr(w, http.StatusBadRequest, err)
			return
		}
		if err := reload(); err != nil {
			writeErr(w, http.StatusInternalServerError, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"drops": drops})
	})
	mux.HandleFunc("POST /api/admin/review", func(w http.ResponseWriter, r *http.Request) {
		n, err := ag.Daily(r.Context())
		view, viewErr := st.AdminView()
		if viewErr != nil {
			writeErr(w, http.StatusInternalServerError, viewErr)
			return
		}
		if err != nil && n == 0 {
			writeJSON(w, http.StatusBadGateway, map[string]any{"admin": withVia(view), "error": err.Error(), "reviewed": n})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"admin": withVia(view), "reviewed": n, "envoy": envoy.Status()})
	})
	mux.HandleFunc("POST /api/agent/review", func(w http.ResponseWriter, r *http.Request) {
		n, err := ag.Review(r.Context())
		if err != nil && n == 0 {
			writeErr(w, http.StatusBadGateway, err)
			return
		}
		detail := ""
		if err != nil {
			detail = err.Error()
		}
		writeJSON(w, http.StatusOK, map[string]any{"reviewed": n, "agent": ag.Snapshot(), "error": detail})
	})
	mux.HandleFunc("POST /api/cards/{id}/decide", func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Action string `json:"action"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			writeErr(w, http.StatusBadRequest, err)
			return
		}
		card, err := st.Decide(r.PathValue("id"), body.Action)
		if err != nil {
			writeErr(w, http.StatusBadRequest, err)
			return
		}
		if err := reload(); err != nil {
			writeErr(w, http.StatusInternalServerError, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"card": card, "envoy": envoy.Status()})
	})
	mux.HandleFunc("POST /api/cards/{id}/reroute", func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Host string `json:"host"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			writeErr(w, http.StatusBadRequest, err)
			return
		}
		card, err := st.SetReroute(r.PathValue("id"), body.Host)
		if err != nil {
			writeErr(w, http.StatusBadRequest, err)
			return
		}
		if err := reload(); err != nil {
			writeErr(w, http.StatusInternalServerError, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"card": card, "envoy": envoy.Status()})
	})
	mux.HandleFunc("POST /api/simulate", func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			DeviceID string `json:"deviceId"`
			Host     string `json:"host"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			writeErr(w, http.StatusBadRequest, err)
			return
		}
		card, outcome, err := st.Simulate(body.DeviceID, body.Host)
		if err != nil {
			writeErr(w, http.StatusBadRequest, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"card": card, "outcome": outcome})
	})
	mux.HandleFunc("GET /api/admin", func(w http.ResponseWriter, r *http.Request) {
		view, err := st.AdminView()
		if err != nil {
			writeErr(w, http.StatusInternalServerError, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"admin": withVia(view), "envoy": envoy.Status()})
	})
	mux.HandleFunc("POST /api/admin/connect", func(w http.ResponseWriter, r *http.Request) {
		snap, err := lan.Inspect()
		if err != nil {
			writeErr(w, http.StatusBadGateway, err)
			return
		}
		neighbors := lanNeighbors(snap)
		if err := st.ConnectRouter(snap.Router, snap.Iface, neighbors); err != nil {
			writeErr(w, http.StatusInternalServerError, err)
			return
		}
		view, err := st.AdminView()
		if err != nil {
			writeErr(w, http.StatusInternalServerError, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"admin": withVia(view), "envoy": envoy.Status()})
	})
	mux.HandleFunc("POST /api/admin/dns", func(w http.ResponseWriter, r *http.Request) {
		if err := path.EnableDNS(); err != nil {
			writeErr(w, http.StatusConflict, err)
			return
		}
		view, err := st.AdminView()
		if err != nil {
			writeErr(w, http.StatusInternalServerError, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"admin": withVia(view), "envoy": envoy.Status()})
	})
	mux.HandleFunc("GET /api/gate", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, gateway.Status())
	})
	mux.HandleFunc("POST /api/gate", func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			On *bool `json:"on"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.On == nil {
			writeErr(w, http.StatusBadRequest, errors.New("send on"))
			return
		}
		if *body.On {
			if err := gateway.Enable(); err != nil {
				writeErr(w, http.StatusConflict, err)
				return
			}
		} else {
			gateway.Disable()
		}
		writeJSON(w, http.StatusOK, gateway.Status())
	})
	mux.HandleFunc("POST /api/admin/steer", func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			On *bool `json:"on"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.On == nil {
			writeErr(w, http.StatusBadRequest, errors.New("send on"))
			return
		}
		if *body.On {
			detail, err := path.EnableSteer()
			if err != nil {
				writeErr(w, http.StatusConflict, err)
				return
			}
			_ = detail
		} else {
			path.DisableSteer()
		}
		view, err := st.AdminView()
		if err != nil {
			writeErr(w, http.StatusInternalServerError, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"admin": withVia(view), "via": viaStatus(viaAddr, path)})
	})
	mux.HandleFunc("POST /api/via/check", func(w http.ResponseWriter, r *http.Request) {
		if !fromLocal(r) {
			writeErr(w, http.StatusForbidden, errors.New("local only"))
			return
		}
		var body struct {
			Addr string `json:"addr"`
			Host string `json:"host"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			writeErr(w, http.StatusBadRequest, err)
			return
		}
		decision, err := via.Decide(st, body.Addr, body.Host)
		if err != nil {
			writeErr(w, http.StatusInternalServerError, err)
			return
		}
		writeJSON(w, http.StatusOK, decision)
	})
	mux.HandleFunc("POST /api/via/note", func(w http.ResponseWriter, r *http.Request) {
		if !fromLocal(r) {
			writeErr(w, http.StatusForbidden, errors.New("local only"))
			return
		}
		var body store.ViaNote
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			writeErr(w, http.StatusBadRequest, err)
			return
		}
		if err := st.NoteVia(body); err != nil {
			writeErr(w, http.StatusBadRequest, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"ok": "true"})
	})
	mux.HandleFunc("POST /api/devices/{id}/via", func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			On *bool `json:"on"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.On == nil {
			writeErr(w, http.StatusBadRequest, errors.New("send on"))
			return
		}
		if err := st.SetViaAno(r.PathValue("id"), *body.On); err != nil {
			writeErr(w, http.StatusBadRequest, err)
			return
		}
		path.RefreshSteer()
		view, err := st.AdminView()
		if err != nil {
			writeErr(w, http.StatusInternalServerError, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"admin": withVia(view), "via": viaStatus(viaAddr, path)})
	})
	mux.HandleFunc("POST /api/probe", func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Host string `json:"host"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			writeErr(w, http.StatusBadRequest, err)
			return
		}
		req, err := http.NewRequest(http.MethodGet, "http://127.0.0.1:10080/", nil)
		if err != nil {
			writeErr(w, http.StatusInternalServerError, err)
			return
		}
		req.Host = body.Host
		client := &http.Client{Timeout: 4 * time.Second}
		resp, err := client.Do(req)
		if err != nil {
			writeErr(w, http.StatusBadGateway, err)
			return
		}
		defer resp.Body.Close()
		buf := make([]byte, 512)
		n, _ := resp.Body.Read(buf)
		writeJSON(w, http.StatusOK, map[string]any{
			"status": resp.StatusCode,
			"action": resp.Header.Get("x-ano-action"),
			"body":   string(buf[:n]),
		})
		if resp.Header.Get("x-ano-action") == "drop" {
			_ = st.NoteBlocked(body.Host)
		}
	})

	addr := ":8647"
	log.Printf("Ano processor listening on %s", addr)
	log.Fatal(http.ListenAndServe(addr, mux))
}

func lanNeighbors(snap lan.Snapshot) []store.LANNeighbor {
	neighbors := make([]store.LANNeighbor, 0, len(snap.Neighbors))
	for _, n := range snap.Neighbors {
		neighbors = append(neighbors, store.LANNeighbor{Name: n.Name, Addr: n.Addr, MAC: n.MAC, Kind: n.Kind, Room: n.Room})
	}
	return neighbors
}

func watchNetwork(st *store.Store, path *via.Service) {
	last := ""
	for {
		snap, err := lan.Inspect()
		if err != nil {
			log.Printf("network scan: %v", err)
		} else if err := st.ObserveRouter(snap.Router, snap.Iface, lanNeighbors(snap)); err != nil {
			log.Printf("network scan: %v", err)
		} else {
			path.RefreshSteer()
			key := snap.Router + " " + fmt.Sprintf("%d", len(snap.Neighbors))
			if key != last {
				log.Printf("read %d devices from %s", len(snap.Neighbors), snap.Router)
				last = key
			}
		}
		time.Sleep(45 * time.Second)
	}
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeErr(w http.ResponseWriter, status int, err error) {
	writeJSON(w, status, map[string]string{"error": err.Error()})
}

func viaStatus(addr string, path *via.Service) map[string]any {
	status := path.Status()
	return map[string]any{
		"addr":        addr,
		"port":        via.ProxyPort,
		"proxy":       status.Proxy,
		"dns":         status.DNS,
		"dnsDetail":   status.DNSDetail,
		"steer":       status.Steer,
		"steerDetail": status.SteerDetail,
	}
}

func fromLocal(r *http.Request) bool {
	ip := via.CallerIP(r.RemoteAddr)
	return ip == "127.0.0.1" || ip == "::1"
}
