package store

import (
	"path/filepath"
	"testing"
)

func TestViaRecordsRealTrafficAndSkipsTheSyntheticTick(t *testing.T) {
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
	view, err := s.AdminView()
	if err != nil {
		t.Fatal(err)
	}
	var id string
	for _, group := range view.Groups {
		for _, device := range group.Devices {
			if device.Addr == "192.168.86.57" {
				id = device.ID
			}
		}
	}
	if id == "" {
		t.Fatal("fire tv was not stored")
	}
	if err := s.SetViaAno(id, true); err != nil {
		t.Fatal(err)
	}
	if _, _, enrolled, err := s.ViaDevice("192.168.86.57"); err != nil || !enrolled {
		t.Fatalf("enrolled %v %v", enrolled, err)
	}
	if err := s.NoteVia(ViaNote{Addr: "192.168.86.57", Host: "device-metrics-us.amazon.com", Protocol: "DNS", Category: "outbound", Bytes: 180}); err != nil {
		t.Fatal(err)
	}
	if err := s.NoteVia(ViaNote{Addr: "192.168.86.57", Host: "video.googlevideo.com", Protocol: "DNS", Category: "stream", Bytes: 220}); err != nil {
		t.Fatal(err)
	}
	if err := s.Collect(); err != nil {
		t.Fatal(err)
	}
	cards, err := s.Cards(false)
	if err != nil {
		t.Fatal(err)
	}
	var leak, stream *Card
	for i := range cards {
		switch {
		case cards[i].Destination == "device-metrics-us.amazon.com" && cards[i].Kind == "tv":
			leak = &cards[i]
		case cards[i].Destination == "video.googlevideo.com" && cards[i].Kind == "tv":
			stream = &cards[i]
		}
	}
	if leak == nil || leak.Category != "outbound" || leak.Kind != "tv" || leak.Observations != 1 || leak.Metrics.BeforeBytes != 180 {
		t.Fatalf("leak %+v", leak)
	}
	if stream == nil || stream.Category != "stream" || stream.Observations != 1 {
		t.Fatalf("stream %+v", stream)
	}
	if _, err := s.Decide(stream.ID, "drop"); err == nil {
		t.Fatal("streaming accepted a refuse")
	}
	pending, err := s.Pending(1)
	if err != nil {
		t.Fatal(err)
	}
	seenLeak, seenStream := false, false
	for _, item := range pending {
		if item.DeviceID == id && item.Destination == "device-metrics-us.amazon.com" {
			seenLeak = true
		}
		if item.DeviceID == id && item.Destination == "video.googlevideo.com" {
			seenStream = true
		}
	}
	if !seenLeak || seenStream {
		t.Fatalf("pending leak %v stream %v", seenLeak, seenStream)
	}
	if _, err := s.Decide(leak.ID, "drop"); err != nil {
		t.Fatal(err)
	}
	status, action, found, err := s.ViaRule(id, "device-metrics-us.amazon.com")
	if err != nil || !found || status != "approved" || action != "drop" {
		t.Fatalf("rule %s %s %v %v", status, action, found, err)
	}
}
