package gate

import (
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
)

func publishScreen() {
	ip := screenIPv4()
	if ip == "" {
		return
	}
	body, err := os.ReadFile("/etc/hosts")
	if err != nil {
		return
	}
	next := hostsWithUI(string(body), ip)
	if next != string(body) {
		_ = os.WriteFile("/etc/hosts", []byte(next), 0o644)
	}
	writeScreenDNS(ip)
}

func writeScreenDNS(ip string) {
	path := "/etc/dnsmasq.d/ano-ui.conf"
	text := fmt.Sprintf("address=/%s/%s\n", UIHost, ip)
	if _, err := os.Stat("/etc/dnsmasq.d/ano.conf"); err != nil {
		text = ScreenDNS(ip)
	}
	old, _ := os.ReadFile(path)
	if string(old) == text {
		return
	}
	if err := os.MkdirAll("/etc/dnsmasq.d", 0o755); err != nil {
		return
	}
	if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
		return
	}
	_ = runRoot("systemctl enable dnsmasq; systemctl restart dnsmasq")
}

func screenIPv4() string {
	ifaces, err := net.Interfaces()
	if err != nil {
		return ""
	}
	var fallback string
	for _, iface := range ifaces {
		if iface.Flags&net.FlagUp == 0 || iface.Flags&net.FlagLoopback != 0 {
			continue
		}
		addrs, err := iface.Addrs()
		if err != nil {
			continue
		}
		for _, addr := range addrs {
			ipnet, ok := addr.(*net.IPNet)
			if !ok {
				continue
			}
			ip := ipnet.IP.To4()
			if ip == nil || !ip.IsGlobalUnicast() {
				continue
			}
			if ip.IsPrivate() {
				return ip.String()
			}
			if fallback == "" {
				fallback = ip.String()
			}
		}
	}
	return fallback
}

func canApply() bool { return true }

// Enable loads nftables and dnsmasq, then turns on forwarding.
func (s *Service) Enable() error {
	plan, err := inspect()
	if err != nil {
		return err
	}
	current, _ := os.ReadFile("/proc/sys/net/ipv4/ip_forward")
	if err := s.apply(plan); err != nil {
		return err
	}
	s.mu.Lock()
	s.on = true
	s.plan = plan
	s.saved = strings.TrimSpace(string(current))
	s.detail = fmt.Sprintf("Envoy carries TCP and UDP from %s. A refused endpoint is dropped for every protocol.", plan.LANNet)
	s.mu.Unlock()
	return nil
}

// Disable removes the Ano rules and restores forwarding.
func (s *Service) Disable() {
	s.mu.Lock()
	saved := s.saved
	if saved != "1" {
		saved = "0"
	}
	s.on = false
	s.detail = "Ano is not the gateway."
	s.mu.Unlock()
	if ip := screenIPv4(); ip != "" {
		_ = os.WriteFile("/etc/dnsmasq.d/ano-ui.conf", []byte(ScreenDNS(ip)), 0o644)
	}
	_ = runRoot(fmt.Sprintf("nft delete table inet ano; sysctl -w net.ipv4.ip_forward=%s; rm -f /etc/dnsmasq.d/ano.conf; systemctl try-restart dnsmasq", saved))
}

// Refresh rewrites the rules after a person changes a decision.
func (s *Service) Refresh() error {
	s.mu.Lock()
	on := s.on
	plan := s.plan
	s.mu.Unlock()
	if !on {
		return nil
	}
	return s.apply(plan)
}

func (s *Service) apply(plan Plan) error {
	ips, names, err := targets(s.store)
	if err != nil {
		return err
	}
	blocks, err := s.store.Blocks()
	if err != nil {
		return err
	}
	covered := map[string]bool{}
	for _, name := range names {
		covered[name] = true
	}
	byPort := map[int][]string{}
	for _, block := range blocks {
		if block.Port == 0 {
			if !covered[block.Name] {
				names = append(names, block.Name)
				covered[block.Name] = true
			}
			continue
		}
		if covered[block.Name] {
			continue
		}
		byPort[block.Port] = append(byPort[block.Port], block.Name)
	}
	ips = uniqueIPs(append(ips, AddressesForNames(names)...))
	var ports []PortDrop
	for port, list := range byPort {
		found := AddressesForNames(list)
		if len(found) == 0 {
			continue
		}
		ports = append(ports, PortDrop{Port: port, IPs: found})
	}
	sort.Slice(ports, func(i, j int) bool { return ports[i].Port < ports[j].Port })
	rules, err := RenderNFT(plan, ips, ports)
	if err != nil {
		return err
	}
	if strings.Contains(rules, "route-to") {
		return fmt.Errorf("the gateway rule is not safe")
	}
	dhcp, err := RenderDHCP(plan, names)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(s.dir, 0o755); err != nil {
		return err
	}
	nftPath := filepath.Join(s.dir, "gate.nft")
	dhcpPath := filepath.Join(s.dir, "dnsmasq.conf")
	if err := os.WriteFile(nftPath, []byte(rules), 0o644); err != nil {
		return err
	}
	if err := os.WriteFile(dhcpPath, []byte(dhcp), 0o644); err != nil {
		return err
	}
	_ = os.WriteFile("/etc/dnsmasq.d/ano-ui.conf", []byte(fmt.Sprintf("address=/%s/%s\n", UIHost, plan.LANAddr)), 0o644)
	script := fmt.Sprintf("nft -f %s && sysctl -w net.ipv4.ip_forward=1 && install -D -m 644 %s /etc/dnsmasq.d/ano.conf && systemctl restart dnsmasq", quote(nftPath), quote(dhcpPath))
	return runRoot(script)
}

func readForwarded() (string, error) {
	return "", nil
}

func parseForwarded(text, lanNet string) []Flow {
	return ParseConntrack(text, lanNet)
}

func inspect() (Plan, error) {
	out, err := exec.Command("ip", "-4", "addr").CombinedOutput()
	if err != nil {
		return Plan{}, fmt.Errorf("could not read the ports")
	}
	route, err := exec.Command("ip", "route", "show", "default").CombinedOutput()
	if err != nil {
		return Plan{}, fmt.Errorf("could not read the router-side port")
	}
	wan := ParseDefaultDev(string(route))
	if wan == "" {
		return Plan{}, fmt.Errorf("could not read the router-side port")
	}
	return Choose(ParseIPAddr(string(out)), wan)
}

func runRoot(shell string) error {
	if os.Geteuid() == 0 {
		out, err := exec.Command("/bin/sh", "-c", shell).CombinedOutput()
		if err != nil {
			return clip(out, err)
		}
		return nil
	}
	out, err := exec.Command("sudo", "-n", "/bin/sh", "-c", shell).CombinedOutput()
	if err != nil {
		return clip(out, err)
	}
	return nil
}

func quote(path string) string {
	return "'" + strings.ReplaceAll(path, "'", "") + "'"
}

func clip(out []byte, err error) error {
	text := strings.TrimSpace(string(out))
	if text == "" {
		return err
	}
	if len(text) > 240 {
		text = text[:240]
	}
	return fmt.Errorf("%s", text)
}
