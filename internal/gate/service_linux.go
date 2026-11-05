package gate

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

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
	s.detail = fmt.Sprintf("Forwarding %s from %s out %s.", plan.LANNet, plan.LAN, plan.WAN)
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
	rules, err := RenderNFT(plan, ips)
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
	script := fmt.Sprintf("nft -f %s && sysctl -w net.ipv4.ip_forward=1 && install -D -m 644 %s /etc/dnsmasq.d/ano.conf && systemctl try-restart dnsmasq", quote(nftPath), quote(dhcpPath))
	return runRoot(script)
}

func readForwarded() (string, error) {
	out, err := exec.Command("conntrack", "-L").CombinedOutput()
	if err != nil && len(out) == 0 {
		return "", err
	}
	return string(out), nil
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
