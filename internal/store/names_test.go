package store

import (
	"path/filepath"
	"testing"
)

func TestTagActivityNamesTheRecordedAddress(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "ano.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if err := s.ConnectRouter("192.168.86.1", "Wi-Fi", []LANNeighbor{{
		Name: "Hall phone", Addr: "192.168.86.40", MAC: "02:00:00:00:00:40", Kind: "phone",
	}}); err != nil {
		t.Fatal(err)
	}
	if err := s.NoteForward(ForwardNote{Addr: "192.168.86.40", Remote: "93.184.216.34", Port: 443, Protocol: "HTTPS", Bytes: 1000}); err != nil {
		t.Fatal(err)
	}
	if err := s.NoteForward(ForwardNote{Addr: "192.168.86.40", Remote: "8.8.4.4", Port: 53, Protocol: "UDP", Bytes: 40}); err != nil {
		t.Fatal(err)
	}
	if err := s.NoteForward(ForwardNote{Addr: "192.168.86.40", Remote: "17.57.144.25", Port: 5223, Protocol: "TCP", Bytes: 80}); err != nil {
		t.Fatal(err)
	}
	list, err := s.UnnamedWeb(10)
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]int{}
	for _, item := range list {
		got[item.Destination] = item.Port
	}
	if len(got) != 2 || got["93.184.216.34:443"] != 443 || got["17.57.144.25:5223"] != 5223 {
		t.Fatalf("unnamed %+v", list)
	}
	if err := s.TagActivity("93.184.216.34:443", "example.com", false); err != nil {
		t.Fatal(err)
	}
	cards, err := s.Cards(false)
	if err != nil {
		t.Fatal(err)
	}
	for _, card := range cards {
		if card.Destination == "93.184.216.34:443" && (card.Activity != "example.com" || card.SelfSigned) {
			t.Fatalf("name %q self-signed %v", card.Activity, card.SelfSigned)
		}
	}
	if err := s.NoteForward(ForwardNote{Addr: "192.168.86.40", Remote: "198.51.100.8", Port: 443, Protocol: "HTTPS", Bytes: 100}); err != nil {
		t.Fatal(err)
	}
	if err := s.TagActivity("198.51.100.8:443", "router.home", true); err != nil {
		t.Fatal(err)
	}
	cards, err = s.Cards(false)
	if err != nil {
		t.Fatal(err)
	}
	var signed, local bool
	for _, card := range cards {
		if card.Destination == "198.51.100.8:443" {
			local = card.Activity == "router.home" && card.SelfSigned
		}
		if card.Destination == "93.184.216.34:443" && !card.SelfSigned {
			signed = true
		}
	}
	if !signed || !local {
		t.Fatalf("groups signed=%v self-signed=%v", signed, local)
	}
	if err := s.TagActivity("17.57.144.25:5223", "courier.push.apple.com", false); err != nil {
		t.Fatal(err)
	}
	cards, err = s.Cards(false)
	if err != nil {
		t.Fatal(err)
	}
	var named bool
	for _, card := range cards {
		if card.Destination == "17.57.144.25:5223" && card.Activity == "courier.push.apple.com" && !card.SelfSigned {
			named = true
		}
	}
	if !named {
		t.Fatal("high port name was not stored")
	}
	left, err := s.UnnamedWeb(10)
	if err != nil {
		t.Fatal(err)
	}
	if len(left) != 0 {
		t.Fatalf("still unnamed %+v", left)
	}
}

func TestSelfSignedAndNoCertificateAreEligibleForDrop(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "ano.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if err := s.ConnectRouter("192.168.86.1", "Wi-Fi", []LANNeighbor{{
		Name: "Hall phone", Addr: "192.168.86.40", MAC: "02:00:00:00:00:40", Kind: "phone",
	}}); err != nil {
		t.Fatal(err)
	}
	if err := s.NoteForward(ForwardNote{Addr: "192.168.86.40", Remote: "198.51.100.8", Port: 443, Protocol: "HTTPS", Bytes: 100}); err != nil {
		t.Fatal(err)
	}
	if err := s.NoteForward(ForwardNote{Addr: "192.168.86.40", Remote: "198.51.100.9", Port: 80, Protocol: "HTTP", Bytes: 100}); err != nil {
		t.Fatal(err)
	}
	if err := s.NoteForward(ForwardNote{Addr: "192.168.86.40", Remote: "93.184.216.34", Port: 443, Protocol: "HTTPS", Bytes: 100}); err != nil {
		t.Fatal(err)
	}
	if err := s.TagActivity("198.51.100.8:443", "router.home", true); err != nil {
		t.Fatal(err)
	}
	if err := s.TagNoCertificate("198.51.100.9:80"); err != nil {
		t.Fatal(err)
	}
	if err := s.TagActivity("93.184.216.34:443", "example.com", false); err != nil {
		t.Fatal(err)
	}
	list, err := s.BareCertificates()
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]string{}
	for _, item := range list {
		got[item.ID] = item.Kind
	}
	var selfID, noneID string
	rows, err := s.db.Query(`SELECT id, destination FROM patterns`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	for rows.Next() {
		var id, dest string
		if err := rows.Scan(&id, &dest); err != nil {
			t.Fatal(err)
		}
		switch dest {
		case "198.51.100.8:443":
			selfID = id
		case "198.51.100.9:80":
			noneID = id
		}
	}
	if got[selfID] != "self-signed" || got[noneID] != "none" || len(got) != 2 {
		t.Fatalf("eligible %+v self %s none %s", got, selfID, noneID)
	}
	if _, err := s.Decide(selfID, "allow_direct"); err != nil {
		t.Fatal(err)
	}
	list, err = s.BareCertificates()
	if err != nil {
		t.Fatal(err)
	}
	for _, item := range list {
		if item.ID == selfID {
			t.Fatal("a person's allow was eligible for drop")
		}
	}
}

func TestADroppedNameCoversEveryAddressAndTheNextOne(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "ano.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if err := s.ConnectRouter("192.168.86.1", "Wi-Fi", []LANNeighbor{{
		Name: "Hall phone", Addr: "192.168.86.40", MAC: "02:00:00:00:00:40", Kind: "phone",
	}}); err != nil {
		t.Fatal(err)
	}
	for _, remote := range []string{"198.51.100.10", "198.51.100.11"} {
		if err := s.NoteForward(ForwardNote{Addr: "192.168.86.40", Remote: remote, Port: 443, Protocol: "HTTPS", Bytes: 100}); err != nil {
			t.Fatal(err)
		}
		if err := s.TagActivity(remote+":443", "courier.push.apple.com", false); err != nil {
			t.Fatal(err)
		}
	}
	n, err := s.DecideName("courier.push.apple.com", "drop")
	if err != nil || n != 2 {
		t.Fatalf("applied %d %v", n, err)
	}
	routes, err := s.EnvoyRoutes()
	if err != nil || len(routes) != 2 {
		t.Fatalf("routes %+v %v", routes, err)
	}
	if err := s.NoteForward(ForwardNote{Addr: "192.168.86.40", Remote: "198.51.100.12", Port: 5223, Protocol: "TCP", Bytes: 80}); err != nil {
		t.Fatal(err)
	}
	if err := s.TagActivity("198.51.100.12:5223", "courier.push.apple.com", false); err != nil {
		t.Fatal(err)
	}
	n, err = s.ApplyNameRules()
	if err != nil || n != 1 {
		t.Fatalf("later address %d %v", n, err)
	}
	routes, err = s.EnvoyRoutes()
	if err != nil || len(routes) != 3 {
		t.Fatalf("later routes %+v %v", routes, err)
	}
	n, err = s.ApplyNameRules()
	if err != nil || n != 0 {
		t.Fatalf("second pass %d %v", n, err)
	}
}

func TestMailPortsAreNotDroppedForAPlaceholderCertificate(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "ano.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if err := s.ConnectRouter("192.168.86.1", "Wi-Fi", []LANNeighbor{{
		Name: "Hall phone", Addr: "192.168.86.40", MAC: "02:00:00:00:00:40", Kind: "phone",
	}}); err != nil {
		t.Fatal(err)
	}
	if err := s.NoteForward(ForwardNote{Addr: "192.168.86.40", Remote: "142.250.141.108", Port: 993, Protocol: "TCP", Bytes: 100}); err != nil {
		t.Fatal(err)
	}
	if err := s.NoteForward(ForwardNote{Addr: "192.168.86.40", Remote: "142.250.141.109", Port: 993, Protocol: "TCP", Bytes: 100}); err != nil {
		t.Fatal(err)
	}
	if err := s.NoteForward(ForwardNote{Addr: "192.168.86.40", Remote: "198.51.100.8", Port: 443, Protocol: "HTTPS", Bytes: 100}); err != nil {
		t.Fatal(err)
	}
	if err := s.TagActivity("142.250.141.108:993", "", true); err != nil {
		t.Fatal(err)
	}
	if err := s.TagActivity("142.250.141.109:993", "", true); err != nil {
		t.Fatal(err)
	}
	if err := s.TagActivity("198.51.100.8:443", "router.home", true); err != nil {
		t.Fatal(err)
	}
	cards, err := s.Cards(false)
	if err != nil {
		t.Fatal(err)
	}
	var kept string
	for _, card := range cards {
		if card.Destination == "142.250.141.109:993" {
			kept = card.ID
		}
		if card.Destination != "198.51.100.8:443" {
			if err := s.RecordVerdict(card.ID, "drop", "Self-signed certificate. AI Inference suggests drop.", true); err != nil {
				t.Fatal(err)
			}
		}
	}
	if _, err := s.Decide(kept, "allow_direct"); err != nil {
		t.Fatal(err)
	}
	list, err := s.BareCertificates()
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 1 {
		t.Fatalf("mail was eligible %+v", list)
	}
	n, err := s.ReleaseMailDrops()
	if err != nil || n != 1 {
		t.Fatalf("released %d %v", n, err)
	}
	routes, err := s.EnvoyRoutes()
	if err != nil {
		t.Fatal(err)
	}
	for _, route := range routes {
		if MailDestination(route.Destination) {
			t.Fatalf("mail still dropped %+v", routes)
		}
	}
	cards, err = s.Cards(false)
	if err != nil {
		t.Fatal(err)
	}
	for _, card := range cards {
		if card.Destination == "142.250.141.109:993" && (card.Action != "allow_direct" || card.Source != "customer") {
			t.Fatalf("person's allow changed %+v", card)
		}
	}
}

func TestPort993IsNamedMail(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "ano.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if err := s.ConnectRouter("192.168.86.1", "Wi-Fi", []LANNeighbor{{
		Name: "Hall phone", Addr: "192.168.86.40", MAC: "02:00:00:00:00:40", Kind: "phone",
	}}); err != nil {
		t.Fatal(err)
	}
	for _, remote := range []string{"142.250.141.108", "142.250.141.109"} {
		if err := s.NoteForward(ForwardNote{Addr: "192.168.86.40", Remote: remote, Port: 993, Protocol: "TCP", Bytes: 40}); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.NoteForward(ForwardNote{Addr: "192.168.86.40", Remote: "198.51.100.8", Port: 443, Protocol: "HTTPS", Bytes: 40}); err != nil {
		t.Fatal(err)
	}
	if err := s.TagActivity("198.51.100.8:443", "example.com", false); err != nil {
		t.Fatal(err)
	}
	n, err := s.NameMail()
	if err != nil || n != 0 {
		t.Fatalf("already named %d %v", n, err)
	}
	cards, err := s.Cards(false)
	if err != nil {
		t.Fatal(err)
	}
	for _, card := range cards {
		switch card.Destination {
		case "142.250.141.108:993", "142.250.141.109:993":
			if card.Activity != "Mail" {
				t.Fatalf("activity %+v", card)
			}
		case "198.51.100.8:443":
			if card.Activity != "example.com" {
				t.Fatalf("web name %+v", card)
			}
		}
	}
	n, err = s.DecideName("Mail", "allow_direct")
	if err != nil || n != 2 {
		t.Fatalf("allow mail %d %v", n, err)
	}
	if err := s.NoteForward(ForwardNote{Addr: "192.168.86.40", Remote: "142.251.2.108", Port: 993, Protocol: "TCP", Bytes: 20}); err != nil {
		t.Fatal(err)
	}
	n, err = s.ApplyNameRules()
	if err != nil || n != 1 {
		t.Fatalf("later mail %d %v", n, err)
	}
}
