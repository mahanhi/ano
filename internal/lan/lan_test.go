package lan

import "testing"

func TestParseRouterAndNeighbors(t *testing.T) {
	route := "   route to: default\n   gateway: 192.168.1.1\ninterface: en0\n"
	arp := "? (192.168.1.1) at aa:bb:cc:dd:ee:ff on en0 ifscope [ethernet]\n" +
		"kitchen-speaker.local (192.168.1.20) at 11:22:33:44:55:66 on en0 ifscope [ethernet]\n" +
		"? (192.168.1.30) at (incomplete) on en0 ifscope [ethernet]\n" +
		"? (192.168.1.8) at aa:bb:cc:00:00:01 on en0 ifscope permanent [ethernet]\n" +
		"? (192.168.1.9) at 1:2:3:4:5:6 on en0 ifscope [ethernet]\n" +
		"hall-phone.local (192.168.1.40) at 66:55:44:33:22:11 on en0 ifscope [ethernet]\n" +
		"other.local (10.1.1.5) at aa:aa:aa:aa:aa:aa on en1 ifscope [ethernet]\n"
	snap := Parse(route, arp)
	if snap.Router != "192.168.1.1" || snap.Iface != "en0" {
		t.Fatalf("router %+v", snap)
	}
	if len(snap.Neighbors) != 3 {
		t.Fatalf("neighbors %+v", snap.Neighbors)
	}
	if snap.Neighbors[0].Kind != "speaker" || snap.Neighbors[2].Kind != "phone" {
		t.Fatalf("kinds %+v", snap.Neighbors)
	}
	if snap.Neighbors[1].MAC != "01:02:03:04:05:06" {
		t.Fatalf("short mac %+v", snap.Neighbors[1])
	}
}

func TestWiFiNeighborsStayWithTheWifiRouter(t *testing.T) {
	arp := "? (192.168.86.1) at aa:bb:cc:dd:ee:01 on en1 ifscope [ethernet]\n" +
		"? (192.168.86.220) at 60:6d:c7:a7:b7:27 on en1 ifscope [ethernet]\n" +
		"? (10.0.0.79) at 11:22:33:44:55:66 on en0 ifscope [ethernet]\n" +
		"? (10.0.0.1) at aa:bb:cc:dd:ee:02 on en0 ifscope [ethernet]\n"
	got := NeighborsOn(arp, []Link{
		{Iface: "en0", Router: "10.0.0.1", Room: "Ethernet"},
		{Iface: "en1", Router: "192.168.86.1", Room: "Wi-Fi"},
	})
	if len(got) != 2 {
		t.Fatalf("neighbors %+v", got)
	}
	if got[0].Room != "Wi-Fi" || got[1].Room != "Ethernet" {
		t.Fatalf("rooms %+v", got)
	}
}

func TestMergeUsesAnnouncedRoles(t *testing.T) {
	got := Merge([]Neighbor{{
		Addr: "192.168.86.57", MAC: "90:39:5f:ad:ac:4b", Room: "Wi-Fi",
	}}, []Hint{{Addr: "192.168.86.57", Name: "Maharajan's Fire TV"}})
	if got[0].Name != "Maharajan's Fire TV" || got[0].Kind != "tv" {
		t.Fatalf("fire tv %+v", got[0])
	}
	ring := Merge([]Neighbor{{Addr: "192.168.86.50", MAC: "aa:bb:cc:00:00:01", Room: "Wi-Fi"}}, []Hint{{Addr: "192.168.86.50", Name: "Front doorbell"}})
	if ring[0].Kind != "camera" {
		t.Fatalf("doorbell %+v", ring[0])
	}
	garage := Merge(nil, []Hint{{Addr: "192.168.86.28", MAC: "0c:1c:57:aa:c4:b6", Name: "Garage door"}})
	if len(garage) != 1 || garage[0].Kind != "garage" {
		t.Fatalf("garage %+v", garage)
	}
	unnamed := Merge([]Neighbor{{Addr: "192.168.86.184", MAC: "a8:bb:50:d8:ca:c2", Room: "Wi-Fi"}}, nil)
	if unnamed[0].Name != "" || unnamed[0].Kind != "unknown" {
		t.Fatalf("unnamed %+v", unnamed[0])
	}
}

func TestKindOfUsesRoleWords(t *testing.T) {
	if KindOf("Kitchen speaker") != "speaker" || KindOf("Hall phone") != "phone" || KindOf("Living Room TV") != "tv" {
		t.Fatal("role words")
	}
	if KindOf("TV Room") != "unknown" || KindOf("XBR-49X830C") != "unknown" || KindOf("WiZ") != "unknown" || KindOf("") != "unknown" {
		t.Fatal("brand and model names stay unknown")
	}
	if HomeKitKind("5") != "light" || HomeKitKind("31") != "tv" || HomeKitKind("1") != "" {
		t.Fatal("homekit")
	}
	if ServiceKind("_raop._tcp") != "speaker" || ServiceKind("_ipp._tcp") != "printer" || ServiceKind("_googlecast._tcp") != "" {
		t.Fatal("service")
	}
}

func TestSharedMACKeepsEachAddress(t *testing.T) {
	arp := "? (192.168.86.34) at 48:a6:b8:2:21:ca on en1 ifscope [ethernet]\n" +
		"? (192.168.86.250) at 48:a6:b8:2:21:ca on en1 ifscope [ethernet]\n"
	got := NeighborsOn(arp, []Link{{Iface: "en1", Router: "192.168.86.1", Room: "Wi-Fi"}})
	if len(got) != 2 {
		t.Fatalf("neighbors %+v", got)
	}
	merged := Merge(got, []Hint{
		{Addr: "192.168.86.34", MAC: "5c:aa:fd:b2:ed:fe", Name: "Sonos One SL", Kind: "speaker"},
		{Addr: "192.168.86.250", MAC: "48:a6:b8:02:21:ca", Name: "Sonos Arc", Kind: "speaker"},
	})
	if merged[0].MAC != "5c:aa:fd:b2:ed:fe" || merged[0].Name != "Sonos One SL" {
		t.Fatalf("one %+v", merged[0])
	}
	if merged[1].MAC != "48:a6:b8:02:21:ca" || merged[1].Name != "Sonos Arc" {
		t.Fatalf("arc %+v", merged[1])
	}
}

func TestRinconMAC(t *testing.T) {
	mac := rinconMAC("uuid:RINCON_48A6B80221CA01400::urn:schemas-upnp-org:device:ZonePlayer:1")
	if mac != "48:a6:b8:02:21:ca" {
		t.Fatalf("mac %q", mac)
	}
}

func TestFireTVLookupReadsAddress(t *testing.T) {
	text := "can be reached at 192-168-86-57.local.:35157 (interface 13)\n" +
		" n=Maharajan\\'s\\ Fire\\ TV at=abc s=0 t=2\n"
	hints := hintsFromLookup("amzn.dmgr:example", "_amzn-wplay._tcp", text)
	if len(hints) != 1 || hints[0].Addr != "192.168.86.57" || hints[0].Name != "Maharajan's Fire TV" || hints[0].Kind != "tv" {
		t.Fatalf("%+v", hints)
	}
}
