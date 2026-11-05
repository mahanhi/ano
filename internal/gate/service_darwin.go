package gate

import (
	"fmt"
	"os/exec"
)

func canApply() bool { return false }

// Enable refuses on macOS. The earlier path restarted this Mac.
func (s *Service) Enable() error {
	return fmt.Errorf("the gateway runs on the Orange Pi, not on this Mac")
}

// Disable clears the local flag. It does not change forwarding.
func (s *Service) Disable() {
	s.mu.Lock()
	s.on = false
	s.detail = "Ano is not the gateway."
	s.mu.Unlock()
}

// Refresh is a no-op on macOS.
func (s *Service) Refresh() error { return nil }

func readForwarded() (string, error) {
	out, err := exec.Command("pfctl", "-s", "state", "-v").CombinedOutput()
	if err != nil {
		return "", err
	}
	return string(out), nil
}

func parseForwarded(text, lanNet string) []Flow {
	return ParseStates(text, lanNet)
}

func inspect() (Plan, error) {
	out, err := exec.Command("ifconfig").CombinedOutput()
	if err != nil {
		return Plan{}, fmt.Errorf("could not read the ports")
	}
	route, err := exec.Command("route", "-n", "get", "default").CombinedOutput()
	if err != nil {
		return Plan{}, fmt.Errorf("could not read the router-side port")
	}
	wan := ParseDefaultDev(string(route))
	if wan == "" {
		return Plan{}, fmt.Errorf("could not read the router-side port")
	}
	return Choose(ParseInterfaces(string(out)), wan)
}
