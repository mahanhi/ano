package lan

import "testing"

func TestParseWifi(t *testing.T) {
	ifconfig := `en1: flags=8863<UP,BROADCAST,SMART,RUNNING,SIMPLEX,MULTICAST> mtu 1500
	ether 7e:2b:e1:b0:43:7e
	inet 192.168.86.192 netmask 0xfffffe00 broadcast 192.168.87.255
`
	arp := `? (192.168.86.1) at 28:bd:89:e8:29:d on en1 ifscope [ethernet]
`
	nic, err := ParseWifi(ifconfig, arp, "en1", "192.168.86.1")
	if err != nil {
		t.Fatal(err)
	}
	if nic.Addr != "192.168.86.192" || nic.MAC != "7e:2b:e1:b0:43:7e" || nic.LAN != "192.168.86.0/23" || nic.GatewayMAC != "28:bd:89:e8:29:0d" {
		t.Fatalf("%+v", nic)
	}
}
