// Package sim plays a lab household through Ano.
// The addresses are the documentation network 192.0.2.0/24.
// It does not read the home LAN, start the gateway, or change the mode.
package sim

import (
	"errors"
	"strings"

	"github.com/ano/ano/internal/store"
)

const (
	router    = "192.0.2.1"
	speakerIP = "192.0.2.20"
	phoneIP   = "192.0.2.40"
	metrics   = "device-metrics-us.amazon.com"
	voice     = "avs-alexa-na.amazon.com"
	streaming = "video.googlevideo.com"
	phoneName = "app-measurement.com"
)

// Result is what the lab household left in the store.
type Result struct {
	Mode        string
	Cards       []store.Card
	StreamStays bool
}

// Play adds two lab devices, records their names, and refuses one usage report.
func Play(st *store.Store) (Result, error) {
	if err := st.ObserveRouter(router, "sim0", []store.LANNeighbor{
		{Name: "Kitchen speaker", Addr: speakerIP, MAC: "02:00:00:00:00:20", Kind: "speaker", Room: "Kitchen"},
		{Name: "Hall phone", Addr: phoneIP, MAC: "02:00:00:00:00:40", Kind: "phone", Room: "Hall"},
	}); err != nil {
		return Result{}, err
	}
	devices, err := st.Devices()
	if err != nil {
		return Result{}, err
	}
	byAddr := map[string]store.Device{}
	for _, device := range devices {
		byAddr[device.Addr] = device
	}
	speaker, ok := byAddr[speakerIP]
	phone, phoneOK := byAddr[phoneIP]
	if !ok || !phoneOK {
		return Result{}, errors.New("the lab devices were not stored")
	}
	if err := st.SetViaAno(speaker.ID, true); err != nil {
		return Result{}, err
	}
	if err := st.SetViaAno(phone.ID, true); err != nil {
		return Result{}, err
	}
	notes := []store.ViaNote{
		{Addr: speakerIP, Host: metrics, Protocol: "HTTPS", Category: "outbound", Port: 443, Bytes: 12000},
		{Addr: speakerIP, Host: voice, Protocol: "HTTPS", Category: "outbound", Port: 443, Bytes: 48000},
		{Addr: speakerIP, Host: streaming, Protocol: "HTTPS", Category: "stream", Port: 443, Bytes: 800000},
		{Addr: phoneIP, Host: phoneName, Protocol: "HTTPS", Category: "outbound", Port: 443, Bytes: 8000},
	}
	for _, note := range notes {
		if err := st.NoteVia(note); err != nil {
			return Result{}, err
		}
	}
	cards, err := st.Cards(false)
	if err != nil {
		return Result{}, err
	}
	var metricsID, streamID string
	for _, card := range cards {
		switch card.Destination {
		case metrics:
			metricsID = card.ID
		case streaming:
			streamID = card.ID
		}
	}
	if metricsID == "" || streamID == "" {
		return Result{}, errors.New("the lab names were not recorded")
	}
	if _, err := st.Decide(metricsID, "drop"); err != nil {
		return Result{}, err
	}
	if err := st.NoteVia(store.ViaNote{Addr: speakerIP, Host: metrics, Protocol: "HTTPS", Category: "outbound", Port: 443, Bytes: 4000, Refused: true}); err != nil {
		return Result{}, err
	}
	_, streamErr := st.Decide(streamID, "drop")
	cards, err = st.Cards(false)
	if err != nil {
		return Result{}, err
	}
	mode, err := st.Mode()
	if err != nil {
		return Result{}, err
	}
	return Result{Mode: mode, Cards: cards, StreamStays: streamErr != nil && strings.Contains(streamErr.Error(), "direct path")}, nil
}
