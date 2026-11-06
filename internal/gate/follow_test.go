package gate

import "testing"

func TestParseAccessLineKeepsTheInternetConnection(t *testing.T) {
	got, ok := ParseAccessLine("192.168.50.57:50000 3.3.3.3:443 20 2000\n")
	if !ok || got.Src != "192.168.50.57" || got.Dst != "3.3.3.3" || got.Port != 443 || got.Protocol != "HTTPS" || got.Bytes != 2020 {
		t.Fatalf("%+v %v", got, ok)
	}
	quic, ok := ParseAccessLine("192.168.50.57:50000 3.3.3.3:443 20 2000 UDP\n")
	if !ok || quic.Protocol != "QUIC" {
		t.Fatalf("%+v %v", quic, ok)
	}
	if _, ok := ParseAccessLine("192.168.50.57:1 192.168.50.1:80 1 1"); ok {
		t.Fatal("a home address was recorded")
	}
	if _, ok := ParseAccessLine("192.168.50.57:1 - 0 0"); ok {
		t.Fatal("a line with no upstream was recorded")
	}
}
