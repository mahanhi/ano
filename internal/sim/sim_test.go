package sim

import (
	"path/filepath"
	"testing"

	"github.com/ano/ano/internal/store"
)

func TestPlayRefusesOneNameAndLeavesTheRest(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "sim.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })

	got, err := Play(st)
	if err != nil {
		t.Fatal(err)
	}
	if got.Mode != "passive" {
		t.Fatalf("mode %s", got.Mode)
	}
	if !got.StreamStays {
		t.Fatal("streaming was refused")
	}
	byName := map[string]store.Card{}
	for _, card := range got.Cards {
		byName[card.Destination] = card
	}
	usage := byName[metrics]
	if usage.Status != "approved" || usage.Action != "drop" || usage.Metrics.BeforeBytes != 12000 || usage.Metrics.AfterBytes != 4000 {
		t.Fatalf("usage %+v", usage.Metrics)
	}
	spoken := byName[voice]
	if spoken.Status == "approved" || spoken.Metrics.BeforeBytes != 48000 {
		t.Fatalf("voice %+v %s", spoken.Metrics, spoken.Status)
	}
	video := byName[streaming]
	if video.Action != "allow_direct" || video.Category != "stream" {
		t.Fatalf("stream %s %s", video.Action, video.Category)
	}
	phone := byName[phoneName]
	if phone.DeviceName != "Hall phone" || phone.Status == "approved" {
		t.Fatalf("phone %+v", phone)
	}
	devices, err := st.Devices()
	if err != nil {
		t.Fatal(err)
	}
	if len(devices) != 2 {
		t.Fatalf("devices %+v", devices)
	}
}

func TestPlayUsesOnlyTheLabNetwork(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "sim.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	if _, err := Play(st); err != nil {
		t.Fatal(err)
	}
	devices, err := st.Devices()
	if err != nil {
		t.Fatal(err)
	}
	for _, device := range devices {
		if len(device.Addr) < 8 || device.Addr[:8] != "192.0.2." {
			t.Fatalf("address %s", device.Addr)
		}
	}
}
