package gate

import (
	"strings"
	"testing"
)

func TestChooseSeparatesTheModemPortFromTheHomePort(t *testing.T) {
	text := "" +
		"en0: flags=8863<UP,BROADCAST,SMART,RUNNING,SIMPLEX,MULTICAST> mtu 1500\n" +
		"\tinet 10.0.0.35 netmask 0xffffff00 broadcast 10.0.0.255\n" +
		"en9: flags=8863<UP,BROADCAST,SMART,RUNNING,SIMPLEX,MULTICAST> mtu 1500\n" +
		"\tinet 10.0.0.57 netmask 0xffffff00 broadcast 10.0.0.255\n" +
		"en1: flags=8863<UP,BROADCAST,SMART,RUNNING,SIMPLEX,MULTICAST> mtu 1500\n" +
		"\tinet 192.168.86.192 netmask 0xfffffe00 broadcast 192.168.87.255\n"
	plan, err := Choose(ParseInterfaces(text), "en0")
	if err != nil {
		t.Fatal(err)
	}
	if plan.WAN != "en0" || plan.WANAddr != "10.0.0.35" || plan.LAN != "en1" || plan.LANAddr != "192.168.86.192" || plan.LANNet != "192.168.86.0/23" {
		t.Fatalf("plan %+v", plan)
	}
	rules, err := RenderNAT(plan)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(rules, "nat on en0 inet from 192.168.86.0/23 to any -> (en0)") || strings.Contains(rules, "route-to") {
		t.Fatal(rules)
	}
	plan.LAN = plan.WAN
	if _, err := RenderNAT(plan); err == nil {
		t.Fatal("same port was accepted")
	}
}

func TestParseStatesKeepsHomeInternetConversations(t *testing.T) {
	text := "" +
		"ALL tcp 192.168.86.57:50000 -> 3.3.3.3:443       ESTABLISHED:ESTABLISHED\n" +
		"   age 00:00:01, expires in 01:00:00, 4:2 pkts, 2000:100 bytes\n" +
		"ALL udp 192.168.86.41:40000 -> 8.8.8.8:53       MULTIPLE:SINGLE\n" +
		"   age 00:00:01, expires in 00:00:30, 1:1 pkts, 60:80 bytes\n" +
		"ALL tcp 10.0.0.35:1 -> 9.9.9.9:443       ESTABLISHED:ESTABLISHED\n" +
		"   age 00:00:01, expires in 01:00:00, 1:1 pkts, 10:10 bytes\n" +
		"ALL tcp 192.168.86.57:1 -> 192.168.86.1:80       ESTABLISHED:ESTABLISHED\n"
	got := ParseStates(text, "192.168.86.0/23")
	if len(got) != 2 || got[0].Src != "192.168.86.57" || got[0].Dst != "3.3.3.3" || got[0].Port != 443 || got[0].Bytes != 2000 || got[1].Port != 53 {
		t.Fatalf("flows %+v", got)
	}
}

func TestRenderNFTForwardsTwoPortsAndDropsAnAddress(t *testing.T) {
	plan := Plan{WAN: "enp1s0", WANAddr: "10.0.0.35", LAN: "enP2p1s0", LANAddr: "192.168.50.1", LANNet: "192.168.50.0/24"}
	rules, err := RenderNFT(plan, []string{"3.3.3.3"}, []PortDrop{{Port: 443, IPs: []string{"9.9.9.9"}}})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(rules, "route-to") || !strings.Contains(rules, `oifname "enp1s0" ip saddr 192.168.50.0/24 masquerade`) || !strings.Contains(rules, "redirect to :10082") || !strings.Contains(rules, "redirect to :10083") || !strings.Contains(rules, "th dport { 53, 853 }") || !strings.Contains(rules, "th dport 443") || !strings.Contains(rules, "9.9.9.9") || !strings.Contains(rules, `oifname "enp1s0" drop`) || !strings.Contains(rules, "3.3.3.3") {
		t.Fatal(rules)
	}
	plan.LAN = plan.WAN
	if _, err := RenderNFT(plan, nil, nil); err == nil {
		t.Fatal("same port was accepted")
	}
	plan.LAN = "enP2p1s0"
	if _, err := RenderNFT(plan, []string{"not-an-ip"}, nil); err == nil {
		t.Fatal("a name was accepted as an address")
	}
}

func TestChooseSkipsTheDockerPort(t *testing.T) {
	ifaces := []Iface{
		{Name: "enp97s0", Addr: "10.0.0.6", CIDR: "10.0.0.0/24"},
		{Name: "docker0", Addr: "172.17.0.1", CIDR: "172.17.0.0/16"},
		{Name: "enp49s0", Addr: "192.168.11.1", CIDR: "192.168.11.0/24"},
	}
	plan, err := Choose(ifaces, "enp97s0")
	if err != nil || plan.LAN != "enp49s0" || plan.LANAddr != "192.168.11.1" {
		t.Fatalf("%v %+v", err, plan)
	}
}

func TestRenderDHCPKeepsTheRouterPortOut(t *testing.T) {
	plan := Plan{WAN: "enp1s0", WANAddr: "10.0.0.35", LAN: "enP2p1s0", LANAddr: "192.168.50.1", LANNet: "192.168.50.0/24"}
	text, err := RenderDHCP(plan, []string{"Device-Metrics.Example.com"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(text, "address=/device-metrics.example.com/") || !strings.Contains(text, "address=/ano.home.security.ai/192.168.50.1") {
		t.Fatal(text)
	}
	if strings.Contains(text, "dhcp-range") || strings.Contains(text, "dhcp-option") || strings.Contains(text, "no-dhcp-interface") || strings.Contains(text, "interface=") {
		t.Fatal(text)
	}
}

func TestScreenDNSAnswersThePiOnly(t *testing.T) {
	text := ScreenDNS("10.0.0.6")
	if text != "address=/ano.home.security.ai/10.0.0.6\n" || strings.Contains(text, "no-dhcp-interface") || strings.Contains(text, "dhcp-range") {
		t.Fatal(text)
	}
}

func TestHostsKeepsTheScreenName(t *testing.T) {
	body := "127.0.0.1 localhost\n10.0.0.9 ano.home.security.ai\n# keep ano.home.security.ai in this note\n"
	got := hostsWithUI(body, "10.0.0.6")
	if strings.Count(got, "ano.home.security.ai") != 2 || !strings.Contains(got, "10.0.0.6 ano.home.security.ai\n") || strings.Contains(got, "10.0.0.9") || !strings.Contains(got, "# keep") {
		t.Fatal(got)
	}
}

func TestParseIPAddrIgnoresLoopback(t *testing.T) {
	text := "" +
		"1: lo: <LOOPBACK,UP,LOWER_UP> mtu 65536\n" +
		"    inet 127.0.0.1/8 scope host lo\n" +
		"2: enp97s0: <BROADCAST,MULTICAST,UP,LOWER_UP> mtu 1500\n" +
		"    inet 10.0.0.6/24 brd 10.0.0.255 scope global enp97s0\n"
	ifaces := ParseIPAddr(text)
	if len(ifaces) != 1 || ifaces[0].Name != "enp97s0" {
		t.Fatalf("ports %+v", ifaces)
	}
	if _, err := Choose(ifaces, "enp97s0"); err == nil {
		t.Fatal("loopback was accepted as the home port")
	}
}

func TestParseIPAddrAndConntrack(t *testing.T) {
	addr := "" +
		"2: enp1s0: <BROADCAST,MULTICAST,UP,LOWER_UP> mtu 1500\n" +
		"    inet 10.0.0.35/24 brd 10.0.0.255 scope global enp1s0\n" +
		"3: enP2p1s0: <BROADCAST,MULTICAST,UP,LOWER_UP> mtu 1500\n" +
		"    inet 192.168.50.1/24 brd 192.168.50.255 scope global enP2p1s0\n"
	ifaces := ParseIPAddr(addr)
	plan, err := Choose(ifaces, ParseDefaultDev("default via 10.0.0.1 dev enp1s0 proto dhcp"))
	if err != nil || plan.WAN != "enp1s0" || plan.LAN != "enP2p1s0" || plan.LANNet != "192.168.50.0/24" {
		t.Fatalf("%v %+v", err, plan)
	}
	text := "tcp      6 100 ESTABLISHED src=192.168.50.57 dst=3.3.3.3 sport=50000 dport=443 packets=4 bytes=2000 src=3.3.3.3 dst=192.168.50.1 sport=443 dport=50000 packets=2 bytes=100\n" +
		"udp      17 20 src=10.0.0.35 dst=9.9.9.9 sport=1 dport=53 packets=1 bytes=40 src=9.9.9.9 dst=10.0.0.35 sport=53 dport=1 packets=1 bytes=40\n"
	got := ParseConntrack(text, plan.LANNet)
	if len(got) != 1 || got[0].Src != "192.168.50.57" || got[0].Port != 443 || got[0].Bytes != 2000 {
		t.Fatalf("flows %+v", got)
	}
}

func TestActiveFlowsDropsFinishedTCP(t *testing.T) {
	text := "tcp 6 100 ESTABLISHED src=192.168.50.57 dst=3.3.3.3 sport=50000 dport=443 packets=4 bytes=2000 src=3.3.3.3 dst=192.168.50.1 sport=443 dport=50000 packets=2 bytes=100\n" +
		"tcp 6 20 TIME_WAIT src=192.168.50.57 dst=8.8.8.8 sport=50001 dport=443 packets=4 bytes=900 src=8.8.8.8 dst=192.168.50.1 sport=443 dport=50001 packets=2 bytes=100\n" +
		"udp 17 20 src=192.168.50.57 dst=1.1.1.1 sport=41000 dport=53 packets=1 bytes=60 src=1.1.1.1 dst=192.168.50.1 sport=53 dport=41000 packets=1 bytes=80\n"
	got := ActiveFlows(text, "192.168.50.0/24")
	if len(got) != 2 {
		t.Fatalf("open flows %+v", got)
	}
	if got[0].Dst != "3.3.3.3" || got[1].Dst != "1.1.1.1" {
		t.Fatalf("open flows %+v", got)
	}
}

func TestActiveFlowsKeepsAConnectionWithoutByteCounters(t *testing.T) {
	text := "ipv4     2 tcp      6 424854 ESTABLISHED src=192.168.11.133 dst=142.250.141.109 sport=55887 dport=993 src=142.250.141.109 dst=10.0.0.6 sport=993 dport=55887 [ASSURED] mark=0 zone=0 use=2\n"
	got := ActiveFlows(text, "192.168.11.0/24")
	if len(got) != 1 || got[0].Dst != "142.250.141.109" || got[0].Port != 993 || got[0].Bytes != 0 {
		t.Fatalf("open flows %+v", got)
	}
}

func TestMulticastStaysOnTheLAN(t *testing.T) {
	if !staysOnTheLAN("224.0.0.251") || !staysOnTheLAN("255.255.255.255") || staysOnTheLAN("1.2.3.4") || staysOnTheLAN("192.168.11.1") {
		t.Fatal("local destinations")
	}
	text := "ipv4     2 udp      17 10 src=192.168.11.133 dst=224.0.0.251 sport=5353 dport=5353 src=224.0.0.251 dst=192.168.11.133 sport=5353 dport=5353 [ASSURED] mark=0 zone=0 use=2\n"
	got := ActiveFlows(text, "192.168.11.0/24")
	if len(got) != 1 || got[0].Dst != "224.0.0.251" || got[0].Port != 5353 || !staysOnTheLAN(got[0].Dst) {
		t.Fatalf("mdns %+v", got)
	}
}
