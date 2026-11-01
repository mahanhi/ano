package flow

import "testing"

func TestParseCountsBytesToTheTelevision(t *testing.T) {
	text := "tcp4  0  0  192.168.86.192.54007  192.168.86.220.8009  ESTABLISHED  42628  12511  rest\n" +
		"udp4  0  0  192.168.86.192.5353  192.168.86.255.5353  100  100\n"
	got := Parse(text)
	if len(got) != 1 {
		t.Fatalf("flows %+v", got)
	}
	if got[0].Remote != "192.168.86.220" || got[0].Service != "Cast" || got[0].Bytes != 55139 || got[0].Protocol != "TCP" {
		t.Fatalf("flow %+v", got[0])
	}
	var meter Meter
	first := meter.Readings(got)
	if len(first) != 1 || first[0].Delta != 55139 {
		t.Fatalf("first %+v", first)
	}
	got[0].Bytes = 56000
	second := meter.Readings(got)
	if len(second) != 1 || second[0].Delta != 861 {
		t.Fatalf("second %+v", second)
	}
}
