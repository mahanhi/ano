package store

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestSpeakerCardsStaySuggestedUntilYouDecide(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "hbar.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if err := s.LoadExample(); err != nil {
		t.Fatal(err)
	}
	only, err := s.SpeakerOnly()
	if err != nil || !only {
		t.Fatalf("speaker only default: %v %v", only, err)
	}
	cards, err := s.Cards(true)
	if err != nil {
		t.Fatal(err)
	}
	if len(cards) != 3 {
		t.Fatalf("speaker cards: %d", len(cards))
	}
	for _, c := range cards {
		if c.Status != "suggested" {
			t.Fatalf("status %s", c.Status)
		}
		if c.Kind != "speaker" {
			t.Fatalf("kind %s", c.Kind)
		}
		if c.ID == "pat-metrics-us" && (c.Metrics.Dropped || c.Metrics.BeforeLookups != 84 || c.Metrics.BeforeBytes != 1008000) {
			t.Fatalf("metrics before a drop: %+v", c.Metrics)
		}
	}
	routes, err := s.EnvoyRoutes()
	if err != nil {
		t.Fatal(err)
	}
	if len(routes) != 0 {
		t.Fatalf("envoy routes before a decision: %d", len(routes))
	}
	if _, err := s.Decide("pat-metrics-us", "drop"); err != nil {
		t.Fatal(err)
	}
	cards, err = s.Cards(true)
	if err != nil {
		t.Fatal(err)
	}
	var dropped Card
	for _, c := range cards {
		if c.ID == "pat-metrics-us" {
			dropped = c
		}
	}
	if !dropped.Metrics.Dropped || dropped.Metrics.BeforeLookups != 84 || dropped.Metrics.BeforeBytes != 1008000 || dropped.Metrics.AfterLookups != 0 {
		t.Fatalf("metrics after drop: %+v", dropped.Metrics)
	}
	if _, _, err := s.Simulate("dev-kitchen-speaker", "device-metrics-us.amazon.com"); err != nil {
		t.Fatal(err)
	}
	cards, err = s.Cards(true)
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range cards {
		if c.ID == "pat-metrics-us" {
			dropped = c
		}
	}
	if dropped.Metrics.BeforeLookups != 84 || dropped.Metrics.AfterLookups != 1 || dropped.Metrics.AfterBytes != 12000 {
		t.Fatalf("metrics after a refused lookup: %+v", dropped.Metrics)
	}
	if err := s.TickSpeaker(); err != nil {
		t.Fatal(err)
	}
	cards, err = s.Cards(true)
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range cards {
		if c.ID == "pat-metrics-us" && (c.Metrics.BeforeLookups != 84 || c.Metrics.AfterLookups != 2) {
			t.Fatalf("live drop tick: %+v", c.Metrics)
		}
		if c.ID == "pat-metrics-us-2" && c.Metrics.BeforeLookups != 41 {
			t.Fatalf("live allow tick: %+v", c.Metrics)
		}
	}
	routes, err = s.EnvoyRoutes()
	if err != nil {
		t.Fatal(err)
	}
	if len(routes) != 1 || routes[0].Destination != "device-metrics-us.amazon.com" || routes[0].Action != "drop" {
		t.Fatalf("routes %+v", routes)
	}
	series, err := s.Series()
	if err != nil {
		t.Fatal(err)
	}
	if len(series) < 70 {
		t.Fatalf("expected backfill plus a live tick, got %d samples", len(series))
	}
	if _, err := s.Decide("pat-metrics-us", "ignore"); err != nil {
		t.Fatal(err)
	}
	routes, err = s.EnvoyRoutes()
	if err != nil {
		t.Fatal(err)
	}
	if len(routes) != 0 {
		t.Fatalf("setting a card aside should unload it, routes %+v", routes)
	}
}

func TestSetRerouteMapsAnyNameThroughEnvoy(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "hbar.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if err := s.LoadExample(); err != nil {
		t.Fatal(err)
	}
	card, err := s.SetReroute("pat-voice", "logs.example.net")
	if err != nil {
		t.Fatal(err)
	}
	if card.Action != "allow_envoy" || card.Status != "approved" || card.Reroute != "logs.example.net" {
		t.Fatalf("mapped card %+v", card)
	}
	routes, err := s.EnvoyRoutes()
	if err != nil {
		t.Fatal(err)
	}
	if len(routes) != 1 || routes[0].Destination != "avs-alexa-na.amazon.com" || routes[0].Reroute != "logs.example.net" {
		t.Fatalf("routes %+v", routes)
	}
	if _, err := s.SetReroute("pat-voice", ""); err != nil {
		t.Fatal(err)
	}
	routes, err = s.EnvoyRoutes()
	if err != nil {
		t.Fatal(err)
	}
	if len(routes) != 1 || routes[0].Reroute != "" {
		t.Fatalf("cleared map should still be a proxy route, %+v", routes)
	}
	if _, err := s.SetReroute("pat-voice", "not a host"); err == nil {
		t.Fatal("expected a hostname error")
	}
}

func TestPastDayShowsCadenceAndSimilarTraffic(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "hbar.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if err := s.LoadExample(); err != nil {
		t.Fatal(err)
	}
	pending, err := s.Pending(1)
	if err != nil {
		t.Fatal(err)
	}
	var metrics, voice, phone ReviewItem
	for _, item := range pending {
		switch item.Destination {
		case "device-metrics-us.amazon.com":
			metrics = item
		case "avs-alexa-na.amazon.com":
			voice = item
		case "app-measurement.com":
			phone = item
		}
	}
	if metrics.DeviceID == "" || metrics.DeviceID == phone.DeviceID {
		t.Fatalf("inference should be per device, metrics %q phone %q", metrics.DeviceID, phone.DeviceID)
	}
	if metrics.Cadence == "" || !strings.Contains(metrics.Cadence, "past 24 hours") {
		t.Fatalf("cadence %q", metrics.Cadence)
	}
	if !strings.Contains(metrics.Similar, "device-metrics-us-2.amazon.com") {
		t.Fatalf("similar %q", metrics.Similar)
	}
	if strings.Contains(metrics.Similar, phone.Destination) {
		t.Fatalf("speaker inference included another device: %q", metrics.Similar)
	}
	if metrics.Protocol != "HTTPS" || voice.Protocol != "HTTPS" || phone.Protocol != "HTTPS" {
		t.Fatalf("protocols metrics %q voice %q phone %q", metrics.Protocol, voice.Protocol, phone.Protocol)
	}
	if !strings.Contains(metrics.SendTime, "throughout the day") {
		t.Fatalf("send time %q", metrics.SendTime)
	}
	if !strings.Contains(voice.SendTime, "07:00") || strings.Contains(voice.SendTime, "throughout the day") {
		t.Fatalf("voice send time %q", voice.SendTime)
	}
	if strings.Contains(phone.Similar, "amazon.com") {
		t.Fatalf("phone inference included the speaker: %q", phone.Similar)
	}
}

func TestPassiveCollectsEveryDeviceAndDoesNotEnforceQwen(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "hbar.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if err := s.LoadExample(); err != nil {
		t.Fatal(err)
	}
	mode, err := s.Mode()
	if err != nil || mode != "passive" {
		t.Fatalf("mode %s %v", mode, err)
	}
	if err := s.Collect(); err != nil {
		t.Fatal(err)
	}
	n, err := s.ObservationCount()
	if err != nil || n != 4 {
		t.Fatalf("observations %d %v", n, err)
	}
	var protocol string
	if err := s.db.QueryRow(`SELECT protocol FROM observations LIMIT 1`).Scan(&protocol); err != nil || protocol != "HTTPS" {
		t.Fatalf("collected protocol %q %v", protocol, err)
	}
	pending, err := s.Pending(1)
	if err != nil || len(pending) != 4 {
		t.Fatalf("pending %d %v", len(pending), err)
	}
	if err := s.RecordVerdict("pat-metrics-us", "drop", "Usage reports can stop.", false); err != nil {
		t.Fatal(err)
	}
	routes, err := s.EnvoyRoutes()
	if err != nil || len(routes) != 0 {
		t.Fatalf("passive verdict enforced routes %+v %v", routes, err)
	}
	if err := s.SetMode("active"); err != nil {
		t.Fatal(err)
	}
	routes, err = s.EnvoyRoutes()
	if err != nil || len(routes) != 1 || routes[0].Destination != "device-metrics-us.amazon.com" {
		t.Fatalf("active routes %+v %v", routes, err)
	}
	if _, err := s.Decide("pat-voice", "allow_direct"); err != nil {
		t.Fatal(err)
	}
	if err := s.RecordVerdict("pat-voice", "drop", "should not override a person", true); err != nil {
		t.Fatal(err)
	}
	cards, err := s.Cards(false)
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range cards {
		if c.ID == "pat-voice" && (c.Source != "customer" || c.Action != "allow_direct") {
			t.Fatalf("person's choice overwritten: %+v", c)
		}
	}
}

func TestOpenStartsFromTheNetwork(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "ano.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	devices, err := s.Devices()
	if err != nil {
		t.Fatal(err)
	}
	if len(devices) != 0 {
		t.Fatalf("devices %+v", devices)
	}
}

func TestConnectRouterGroupsDevicesAndTraffic(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "ano.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if err := s.ConnectRouter("192.168.1.1", "en0", []LANNeighbor{
		{Name: "kitchen-speaker.local", Addr: "192.168.1.20", MAC: "11:22:33:44:55:66", Kind: "speaker"},
		{Name: "hall-phone.local", Addr: "192.168.1.40", MAC: "66:55:44:33:22:11", Kind: "phone"},
	}); err != nil {
		t.Fatal(err)
	}
	view, err := s.AdminView()
	if err != nil {
		t.Fatal(err)
	}
	if !view.Connected || view.RouterAddr != "192.168.1.1" {
		t.Fatalf("router %+v", view)
	}
	var speaker, phone bool
	for _, group := range view.Groups {
		for _, device := range group.Devices {
			if device.MAC == "11:22:33:44:55:66" {
				speaker = device.Kind == "speaker" && device.OnNetwork
			}
			if device.MAC == "66:55:44:33:22:11" {
				phone = device.Kind == "phone" && device.OnNetwork && device.Addr == "192.168.1.40"
			}
		}
	}
	if !speaker || !phone {
		t.Fatalf("groups %+v", view.Groups)
	}
}

func TestNoteFlowsRecordsBytesWithoutInventingMore(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "ano.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if err := s.ConnectRouter("192.168.86.1", "Wi-Fi", []LANNeighbor{{
		Name: "XBR-49X830C", Addr: "192.168.86.220", MAC: "60:6d:c7:a7:b7:27", Kind: "tv",
	}}); err != nil {
		t.Fatal(err)
	}
	if err := s.NoteFlows([]FlowNote{{Addr: "192.168.86.220", Service: "Cast", Protocol: "TCP", Delta: 55139}}); err != nil {
		t.Fatal(err)
	}
	if err := s.Collect(); err != nil {
		t.Fatal(err)
	}
	view, err := s.AdminView()
	if err != nil {
		t.Fatal(err)
	}
	var found bool
	for _, group := range view.Groups {
		for _, device := range group.Devices {
			if device.Addr == "192.168.86.220" {
				found = device.Names == 1 && device.BytesLeft == 55139
			}
		}
	}
	if !found {
		t.Fatalf("tv traffic %+v", view.Groups)
	}
	cards, err := s.Cards(false)
	if err != nil {
		t.Fatal(err)
	}
	for _, card := range cards {
		if card.Destination == "Cast" && card.Category != "seen" {
			t.Fatalf("card %+v", card)
		}
	}
}
