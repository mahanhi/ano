package store

import (
	"path/filepath"
	"testing"
)

func TestNoteForwardRecordsTheInternetEndpoint(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "ano.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if err := s.ConnectRouter("192.168.86.1", "Wi-Fi", []LANNeighbor{{
		Name: "Maharajan's Fire TV", Addr: "192.168.86.57", MAC: "90:39:5f:ad:ac:4b", Kind: "tv",
	}}); err != nil {
		t.Fatal(err)
	}
	if err := s.NoteForward(ForwardNote{Addr: "192.168.86.57", Remote: "3.3.3.3", Port: 443, Protocol: "TCP", Bytes: 2000}); err != nil {
		t.Fatal(err)
	}
	if err := s.NoteForward(ForwardNote{Addr: "10.9.9.9", Remote: "3.3.3.3", Port: 443, Protocol: "TCP", Bytes: 50}); err != nil {
		t.Fatal(err)
	}
	cards, err := s.Cards(false)
	if err != nil {
		t.Fatal(err)
	}
	var found *Card
	for i := range cards {
		if cards[i].Destination == "3.3.3.3:443" {
			found = &cards[i]
		}
	}
	if found == nil || found.DeviceName != "Maharajan's Fire TV" || found.Port != 443 || found.Metrics.BeforeBytes != 2000 || found.Category != "seen" {
		t.Fatalf("card %+v", found)
	}
}
