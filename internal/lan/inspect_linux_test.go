//go:build linux

package lan

import "testing"

func TestHomePortSkipsTheRouterSideAndDocker(t *testing.T) {
	text := "" +
		"3: enp97s0    inet 10.0.0.6/24 brd 10.0.0.255 scope global dynamic enp97s0\n" +
		"4: docker0    inet 172.17.0.1/16 brd 172.17.255.255 scope global docker0\n" +
		"5: enp49s0    inet 192.168.11.1/24 brd 192.168.11.255 scope global enp49s0\n"
	iface, self := homePort(text, "enp97s0")
	if iface != "enp49s0" || self != "192.168.11.1" {
		t.Fatalf("%s %s", iface, self)
	}
}
