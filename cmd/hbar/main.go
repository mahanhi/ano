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

	"github.com/hbar/hbar/internal/agent"
	"github.com/hbar/hbar/internal/envoyrun"
	"github.com/hbar/hbar/internal/lan"
	"github.com/hbar/hbar/internal/llm"
	"github.com/hbar/hbar/internal/store"
	"github.com/hbar/hbar/internal/via"
)

func main() {
	dataDir := os.Getenv("HBAR_DATA")
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
	ag := &agent.Agent{
		Store: st,
		Model: llm.Client{Settings: llm.FromEnv()},
		OnEnforce: func() error {
			return envoy.Reload()
		},
	}
	go ag.Loop(context.Background())
	go watchNetwork(st, path)

	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/health", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"ok": "true", "product": "Ano"})
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
		if err := envoy.Reload(); err != nil {
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
		if err := envoy.Reload(); err != nil {
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
			"action": resp.Header.Get("x-hbar-action"),
			"body":   string(buf[:n]),
		})
		if resp.Header.Get("x-hbar-action") == "drop" {
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
