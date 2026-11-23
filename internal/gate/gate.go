// Package gate is Ano as the gateway between a home network and the modem.
// Devices send internet packets to Ano. Ano records them and forwards them
// out the modem port. It does not pretend to be the existing router.
package gate

import (
	"fmt"
	"math/bits"
	"net"
	"regexp"
	"strconv"
	"strings"

	"github.com/ano/ano/internal/envoycfg"
)

// Iface is one port with an IPv4 address.
type Iface struct {
	Name string
	Addr string
	CIDR string
}

// Plan is the modem port and the home port.
type Plan struct {
	WAN     string
	WANAddr string
	LAN     string
	LANAddr string
	LANNet  string
}

// Flow is one forwarded internet conversation.
type Flow struct {
	Src   string
	Dst   string
	Port  int
	Proto string
	Bytes int
}

var stateLine = regexp.MustCompile(`(?i)\b(tcp|udp)\s+([0-9.]+):(\d+)\s+->\s+([0-9.]+):(\d+)`)
var byteLine = regexp.MustCompile(`(\d+):(\d+)\s+bytes`)

// ParseInterfaces reads ifconfig text into ports that have an IPv4 address.
func ParseInterfaces(text string) []Iface {
	var out []Iface
	var name string
	for _, line := range strings.Split(text, "\n") {
		if line != "" && !strings.HasPrefix(line, "\t") && !strings.HasPrefix(line, " ") {
			name = strings.TrimSuffix(strings.Fields(line)[0], ":")
			continue
		}
		fields := strings.Fields(strings.TrimSpace(line))
		if len(fields) < 4 || fields[0] != "inet" || fields[2] != "netmask" || name == "" {
			continue
		}
		ip := net.ParseIP(fields[1])
		cidr, err := network(fields[1], fields[3])
		if err != nil || ip == nil || ip.IsLoopback() || name == "lo" || name == "lo0" {
			continue
		}
		out = append(out, Iface{Name: name, Addr: fields[1], CIDR: cidr})
	}
	return out
}

// Choose picks the modem port and a home port on a different network.
func Choose(ifaces []Iface, wanName string) (Plan, error) {
	var wan *Iface
	for i := range ifaces {
		if ifaces[i].Name == wanName {
			wan = &ifaces[i]
		}
	}
	if wan == nil {
		return Plan{}, fmt.Errorf("the modem port %s has no address", wanName)
	}
	var lan *Iface
	for i := range ifaces {
		if ifaces[i].Name == wan.Name || ifaces[i].CIDR == wan.CIDR || virtualIface(ifaces[i].Name) {
			continue
		}
		if lan == nil || privateRank(ifaces[i].Addr) > privateRank(lan.Addr) {
			lan = &ifaces[i]
		}
	}
	if lan == nil {
		return Plan{}, fmt.Errorf("the home port has to be a different network from %s", wan.CIDR)
	}
	return Plan{WAN: wan.Name, WANAddr: wan.Addr, LAN: lan.Name, LANAddr: lan.Addr, LANNet: lan.CIDR}, nil
}

// RenderNAT is the forward rule from the home network out the modem port.
// The home port and the modem port have to be different. There is no route-to.
func RenderNAT(plan Plan) (string, error) {
	if !safeIface(plan.WAN) || !safeIface(plan.LAN) || plan.WAN == plan.LAN {
		return "", fmt.Errorf("the modem port and the home port have to be two different ports")
	}
	if _, ipnet, err := net.ParseCIDR(plan.LANNet); err != nil || !ipnet.Contains(net.ParseIP(plan.LANAddr)) {
		return "", fmt.Errorf("home network %s", plan.LANNet)
	}
	if net.ParseIP(plan.WANAddr) == nil {
		return "", fmt.Errorf("modem address %s", plan.WANAddr)
	}
	return fmt.Sprintf("nat on %s inet from %s to any -> (%s)\npass out on %s inet from %s to any keep state\n",
		plan.WAN, plan.LANNet, plan.WAN, plan.WAN, plan.LANNet), nil
}

// PortDrop refuses one outside address on a single port.
type PortDrop struct {
	Port int
	IPs  []string
}

// RenderNFT is the Linux forward rule for Ano. The two ports have
// to differ. Dropped addresses are refused. There is no route-to.
func RenderNFT(plan Plan, drops []string, ports []PortDrop) (string, error) {
	if err := checkPlan(plan); err != nil {
		return "", err
	}
	var ips []string
	for _, drop := range drops {
		ip := net.ParseIP(strings.TrimSpace(drop))
		if ip == nil || ip.To4() == nil {
			return "", fmt.Errorf("drop address %s", drop)
		}
		ips = append(ips, ip.String())
	}
	var b strings.Builder
	b.WriteString("table inet ano {\n")
	if len(ips) > 0 {
		b.WriteString("  set drop4 {\n    type ipv4_addr\n    elements = { ")
		b.WriteString(strings.Join(ips, ", "))
		b.WriteString(" }\n  }\n")
	}
	b.WriteString("  chain prerouting {\n    type nat hook prerouting priority dstnat; policy accept;\n")
	if len(ips) > 0 {
		fmt.Fprintf(&b, "    iifname %q ip daddr @drop4 drop\n", plan.LAN)
	}
	for _, drop := range ports {
		if drop.Port < 1 || drop.Port > 65535 || len(drop.IPs) == 0 {
			return "", fmt.Errorf("drop port %d", drop.Port)
		}
		var addrs []string
		for _, raw := range drop.IPs {
			ip := net.ParseIP(strings.TrimSpace(raw))
			if ip == nil || ip.To4() == nil {
				return "", fmt.Errorf("drop address %s", raw)
			}
			addrs = append(addrs, ip.String())
		}
		fmt.Fprintf(&b, "    iifname %q ip daddr { %s } meta l4proto { tcp, udp } th dport %d drop\n", plan.LAN, strings.Join(addrs, ", "), drop.Port)
	}
	fmt.Fprintf(&b, "    iifname %q meta l4proto { tcp, udp } th dport { 53, 853 } ip daddr != %s drop\n", plan.LAN, plan.LANNet)
	fmt.Fprintf(&b, "    iifname %q meta l4proto tcp ip daddr != %s ip daddr != 255.255.255.255 ip daddr != 224.0.0.0/4 redirect to :%d\n", plan.LAN, plan.LANNet, envoycfg.EgressPort)
	fmt.Fprintf(&b, "    iifname %q meta l4proto udp ip daddr != %s ip daddr != 255.255.255.255 ip daddr != 224.0.0.0/4 redirect to :%d\n  }\n", plan.LAN, plan.LANNet, envoycfg.EgressUDPPort)
	b.WriteString("  chain forward {\n    type filter hook forward priority filter; policy accept;\n")
	if len(ips) > 0 {
		b.WriteString("    ip daddr @drop4 drop\n")
	}
	fmt.Fprintf(&b, "    iifname %q oifname %q meta l4proto icmp accept\n", plan.LAN, plan.WAN)
	fmt.Fprintf(&b, "    iifname %q oifname %q drop\n", plan.LAN, plan.WAN)
	fmt.Fprintf(&b, "    iifname %q oifname %q ct state established,related accept\n", plan.WAN, plan.LAN)
	b.WriteString("  }\n  chain postrouting {\n    type nat hook postrouting priority srcnat; policy accept;\n")
	fmt.Fprintf(&b, "    oifname %q ip saddr %s masquerade\n", plan.WAN, plan.LANNet)
	b.WriteString("  }\n}\n")
	return b.String(), nil
}

// UIHost is the name the home network uses for the Ano screen.
const UIHost = "ano.home.security.ai"

// RenderDHCP adds DNS answers only. The home DHCP server stays in the
// administrator's dnsmasq.conf. Refused names are NXDOMAIN. The screen name
// answers with the home address.
func RenderDHCP(plan Plan, names []string) (string, error) {
	if err := checkPlan(plan); err != nil {
		return "", err
	}
	var b strings.Builder
	for _, name := range names {
		clean := strings.TrimSuffix(strings.ToLower(strings.TrimSpace(name)), ".")
		if clean == UIHost {
			continue
		}
		if !safeName(clean) {
			return "", fmt.Errorf("drop name %s", name)
		}
		fmt.Fprintf(&b, "address=/%s/\n", clean)
	}
	b.WriteString(ScreenDNS(plan.LANAddr))
	return b.String(), nil
}

// ScreenDNS is the dnsmasq line for the screen name. It does not hand out addresses.
func ScreenDNS(ip string) string {
	return fmt.Sprintf("address=/%s/%s\n", UIHost, ip)
}

// hostsWithUI returns /etc/hosts with one line for the Ano screen.
func hostsWithUI(body, ip string) string {
	var kept []string
	for _, line := range strings.Split(body, "\n") {
		trim := strings.TrimSpace(line)
		if trim != "" && !strings.HasPrefix(trim, "#") && hostField(trim, UIHost) {
			continue
		}
		kept = append(kept, line)
	}
	for len(kept) > 0 && kept[len(kept)-1] == "" {
		kept = kept[:len(kept)-1]
	}
	return strings.Join(kept, "\n") + "\n" + ip + " " + UIHost + "\n"
}

func hostField(line, host string) bool {
	fields := strings.Fields(line)
	for _, field := range fields[1:] {
		if field == host {
			return true
		}
	}
	return false
}

// ParseIPAddr reads `ip -4 addr` text into ports that have an IPv4 address.
func ParseIPAddr(text string) []Iface {
	var out []Iface
	var name string
	for _, line := range strings.Split(text, "\n") {
		trim := strings.TrimSpace(line)
		if trim == "" {
			continue
		}
		if len(line) > 0 && line[0] >= '0' && line[0] <= '9' {
			fields := strings.Split(trim, ":")
			if len(fields) >= 2 {
				name = strings.TrimSpace(fields[1])
			}
		}
		fields := strings.Fields(trim)
		if len(fields) < 2 || fields[0] != "inet" || name == "" {
			continue
		}
		ip, network, err := net.ParseCIDR(fields[1])
		if err != nil || ip.To4() == nil || ip.IsLoopback() || name == "lo" {
			continue
		}
		out = append(out, Iface{Name: name, Addr: ip.To4().String(), CIDR: network.String()})
	}
	return out
}

// ParseDefaultDev reads the interface from `ip route` or `route -n get default`.
func ParseDefaultDev(text string) string {
	for _, line := range strings.Split(text, "\n") {
		fields := strings.Fields(strings.TrimSpace(line))
		for i := 0; i < len(fields)-1; i++ {
			if fields[i] == "dev" || fields[i] == "interface:" {
				return fields[i+1]
			}
		}
	}
	return ""
}

var connLine = regexp.MustCompile(`\b(tcp|udp)\b.*?\bsrc=([0-9.]+) dst=([0-9.]+) sport=\d+ dport=(\d+)`)
var connBytes = regexp.MustCompile(`\bbytes=(\d+)`)

// ActiveFlows keeps conversations that are open now. A finished TCP state is collected history, not a live connection.
func ActiveFlows(text, lanNet string) []Flow {
	var open strings.Builder
	for _, line := range strings.Split(text, "\n") {
		if openConn(line) {
			open.WriteString(line)
			open.WriteByte('\n')
		}
	}
	return ParseConntrack(open.String(), lanNet)
}

func openConn(line string) bool {
	lower := strings.ToLower(line)
	if !strings.Contains(lower, "src=") {
		return false
	}
	for _, done := range []string{"time_wait", "close_wait", "fin_wait", "last_ack", "syn_sent", "syn_recv", " close "} {
		if strings.Contains(lower, done) {
			return false
		}
	}
	tcp := strings.Contains(lower, " tcp ") || strings.HasPrefix(strings.TrimSpace(lower), "tcp ")
	if tcp && !strings.Contains(lower, "established") {
		return false
	}
	return true
}

// staysOnTheLAN is multicast or broadcast. Those packets are not sent to Envoy, so they need their own record.
func staysOnTheLAN(dst string) bool {
	ip := net.ParseIP(dst)
	if ip == nil {
		return false
	}
	return ip.IsMulticast() || ip.Equal(net.IPv4bcast)
}

// ParseConntrack reads conntrack lines and keeps conversations that start in the home network.
func ParseConntrack(text, lanNet string) []Flow {
	_, ipnet, err := net.ParseCIDR(lanNet)
	if err != nil {
		return nil
	}
	var out []Flow
	for _, line := range strings.Split(text, "\n") {
		loc := connLine.FindStringSubmatchIndex(line)
		if loc == nil {
			continue
		}
		m := connLine.FindStringSubmatch(line)
		src := net.ParseIP(m[2])
		dst := net.ParseIP(m[3])
		if src == nil || dst == nil || !ipnet.Contains(src) || ipnet.Contains(dst) {
			continue
		}
		port, err := strconv.Atoi(m[4])
		if err != nil || port < 1 || port > 65535 {
			continue
		}
		rest := line[loc[1]:]
		if cut := strings.Index(rest, " src="); cut >= 0 {
			rest = rest[:cut]
		}
		n := 0
		if b := connBytes.FindStringSubmatch(rest); b != nil {
			n, _ = strconv.Atoi(b[1])
		}
		out = append(out, Flow{Src: src.String(), Dst: dst.String(), Port: port, Proto: strings.ToUpper(m[1]), Bytes: n})
	}
	return out
}

func checkPlan(plan Plan) error {
	if !safeIface(plan.WAN) || !safeIface(plan.LAN) || plan.WAN == plan.LAN {
		return fmt.Errorf("the router port and the access-point port have to be two different ports")
	}
	if _, ipnet, err := net.ParseCIDR(plan.LANNet); err != nil || !ipnet.Contains(net.ParseIP(plan.LANAddr)) {
		return fmt.Errorf("home network %s", plan.LANNet)
	}
	if net.ParseIP(plan.WANAddr) == nil {
		return fmt.Errorf("router-side address %s", plan.WANAddr)
	}
	return nil
}

func safeName(name string) bool {
	name = strings.TrimSuffix(strings.ToLower(strings.TrimSpace(name)), ".")
	if name == "" || len(name) > 253 || strings.Contains(name, "..") {
		return false
	}
	for _, r := range name {
		if (r < 'a' || r > 'z') && (r < '0' || r > '9') && r != '.' && r != '-' {
			return false
		}
	}
	return strings.Contains(name, ".")
}

// ParseStates reads pf state text and keeps conversations that start in the home network.
func ParseStates(text, lanNet string) []Flow {
	_, ipnet, err := net.ParseCIDR(lanNet)
	if err != nil {
		return nil
	}
	lines := strings.Split(text, "\n")
	var out []Flow
	for i, line := range lines {
		m := stateLine.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		src := net.ParseIP(m[2])
		dst := net.ParseIP(m[4])
		if src == nil || dst == nil || !ipnet.Contains(src) || ipnet.Contains(dst) {
			continue
		}
		port, err := strconv.Atoi(m[5])
		if err != nil || port < 1 || port > 65535 {
			continue
		}
		flow := Flow{Src: src.String(), Dst: dst.String(), Port: port, Proto: strings.ToUpper(m[1])}
		if i+1 < len(lines) {
			if b := byteLine.FindStringSubmatch(lines[i+1]); b != nil {
				n, _ := strconv.Atoi(b[1])
				flow.Bytes = n
			}
		}
		out = append(out, flow)
	}
	return out
}

func network(addr, mask string) (string, error) {
	ip := net.ParseIP(addr).To4()
	if ip == nil {
		return "", fmt.Errorf("address %s", addr)
	}
	raw := strings.TrimPrefix(strings.ToLower(mask), "0x")
	v, err := strconv.ParseUint(raw, 16, 32)
	if err != nil {
		return "", err
	}
	ones := bits.OnesCount32(uint32(v))
	if ones < 8 || ones > 30 {
		return "", fmt.Errorf("mask %s", mask)
	}
	n := ip.Mask(net.CIDRMask(ones, 32))
	return fmt.Sprintf("%s/%d", n.String(), ones), nil
}

func virtualIface(name string) bool {
	switch {
	case name == "docker0" || strings.HasPrefix(name, "docker") || strings.HasPrefix(name, "veth") || strings.HasPrefix(name, "br-") || strings.HasPrefix(name, "virbr"):
		return true
	default:
		return false
	}
}

func privateRank(addr string) int {
	ip := net.ParseIP(addr)
	if ip == nil || !ip.IsPrivate() {
		return 0
	}
	return 1
}

func safeIface(name string) bool {
	if name == "" || len(name) > 15 {
		return false
	}
	for _, r := range name {
		if (r < 'a' || r > 'z') && (r < 'A' || r > 'Z') && (r < '0' || r > '9') {
			return false
		}
	}
	return true
}
