package via

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"net"

	"github.com/hbar/hbar/internal/lan"
	"github.com/hbar/hbar/internal/store"
)

// SteerDevice is one television whose Wi-Fi path Ano is taking.
type SteerDevice struct {
	Addr string `json:"addr"`
	MAC  string `json:"mac"`
}

// SteerConfig is the file the privileged helper reads.
type SteerConfig struct {
	Iface      string        `json:"iface"`
	Gateway    string        `json:"gateway"`
	GatewayMAC string        `json:"gatewayMac"`
	OurMAC     string        `json:"ourMac"`
	OurIP      string        `json:"ourIp"`
	LAN        string        `json:"lan"`
	Listen     string        `json:"listen"`
	Ready      string        `json:"ready"`
	Stop       string        `json:"stop"`
	Ano        string        `json:"ano"`
	Devices    []SteerDevice `json:"devices"`
}

// EnableSteer brings added Fire TVs through this computer. The television
// keeps its own DNS. Streaming video is forwarded. Other names are recorded.
func (s *Service) EnableSteer() (string, error) {
	if s.steerOn() {
		return "Fire TVs already reach the network through this computer.", nil
	}
	nic, err := lan.WifiNIC()
	if err != nil {
		return "", err
	}
	targets, err := fireTVs(s.store)
	if err != nil {
		return "", err
	}
	if len(targets) == 0 {
		return "", fmt.Errorf("add a Fire TV on the device page first")
	}
	if err := os.MkdirAll(s.dir, 0o755); err != nil {
		return "", err
	}
	bin := filepath.Join(s.dir, "hbar-steer")
	build := exec.Command("go", "build", "-o", bin, "./cmd/hbar-steer")
	build.Dir = repoDir()
	if out, err := build.CombinedOutput(); err != nil {
		return "", fmt.Errorf("could not prepare the path helper: %s", strings.TrimSpace(string(out)))
	}
	_ = os.Remove(filepath.Join(s.dir, "via-steer.stop"))
	_ = os.Remove(filepath.Join(s.dir, "via-steer.ready"))
	cfg := SteerConfig{
		Iface: nic.Iface, Gateway: nic.Gateway, GatewayMAC: nic.GatewayMAC,
		OurMAC: nic.MAC, OurIP: nic.Addr, LAN: nic.LAN,
		Listen:  net.JoinHostPort(nic.Addr, fmt.Sprintf("%d", SteerPort)),
		Ready:   filepath.Join(s.dir, "via-steer.ready"),
		Stop:    filepath.Join(s.dir, "via-steer.stop"),
		Ano:     "http://127.0.0.1:8647",
		Devices: targets,
	}
	path := filepath.Join(s.dir, "via-steer.json")
	if err := writeConfig(path, cfg); err != nil {
		return "", err
	}
	logPath := filepath.Join(s.dir, "via-steer.log")
	launch := fmt.Sprintf("nohup %s %s >> %s 2>&1 &", quote(bin), quote(path), quote(logPath))
	if err := runRoot(launch); err != nil {
		return "", err
	}
	deadline := time.Now().Add(8 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(cfg.Ready); err == nil {
			return "Fire TVs now reach the network through this computer. Streaming stays direct. Other names are monitored.", nil
		}
		time.Sleep(200 * time.Millisecond)
	}
	detail := "The path did not start."
	if b, err := os.ReadFile(logPath); err == nil && len(strings.TrimSpace(string(b))) > 0 {
		lines := strings.Split(strings.TrimSpace(string(b)), "\n")
		detail = lines[len(lines)-1]
	}
	_ = os.WriteFile(cfg.Stop, []byte("stop\n"), 0o644)
	return "", fmt.Errorf("%s", detail)
}

// DisableSteer sends the televisions back to the router.
func (s *Service) DisableSteer() {
	path := filepath.Join(s.dir, "via-steer.stop")
	_ = os.WriteFile(path, []byte("stop\n"), 0o644)
	deadline := time.Now().Add(4 * time.Second)
	for time.Now().Before(deadline) {
		if !s.steerOn() {
			return
		}
		time.Sleep(200 * time.Millisecond)
	}
}

// RefreshSteer rewrites the television list while the path is on.
func (s *Service) RefreshSteer() {
	if !s.steerOn() {
		return
	}
	nic, err := lan.WifiNIC()
	if err != nil {
		return
	}
	targets, err := fireTVs(s.store)
	if err != nil {
		return
	}
	cfg := SteerConfig{
		Iface: nic.Iface, Gateway: nic.Gateway, GatewayMAC: nic.GatewayMAC,
		OurMAC: nic.MAC, OurIP: nic.Addr, LAN: nic.LAN,
		Listen:  net.JoinHostPort(nic.Addr, fmt.Sprintf("%d", SteerPort)),
		Ready:   filepath.Join(s.dir, "via-steer.ready"),
		Stop:    filepath.Join(s.dir, "via-steer.stop"),
		Ano:     "http://127.0.0.1:8647",
		Devices: targets,
	}
	_ = writeConfig(filepath.Join(s.dir, "via-steer.json"), cfg)
}

func (s *Service) steerOn() bool {
	_, err := os.Stat(filepath.Join(s.dir, "via-steer.ready"))
	return err == nil
}

func (s *Service) steerDetail() string {
	if s.steerOn() {
		return "Fire TVs reach the network through this computer. Streaming stays direct. Other names are monitored. Stop returns them to the router."
	}
	return "A Fire TV can keep its DNS. Bring it through Ano from this computer, and only the televisions you added are affected."
}

func fireTVs(st *store.Store) ([]SteerDevice, error) {
	rows, err := st.ViaTargets()
	if err != nil {
		return nil, err
	}
	var out []SteerDevice
	for _, row := range rows {
		if row.Kind != "tv" || !strings.Contains(strings.ToLower(row.Name), "fire tv") {
			continue
		}
		mac, ok := lan.CanonicalMAC(row.MAC)
		if !ok || net.ParseIP(row.Addr) == nil {
			continue
		}
		out = append(out, SteerDevice{Addr: row.Addr, MAC: mac})
	}
	return out, nil
}

func writeConfig(path string, cfg SteerConfig) error {
	b, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

func runRoot(shell string) error {
	cmd := exec.Command("sudo", "-n", "/bin/sh", "-c", shell)
	out, err := cmd.CombinedOutput()
	if err == nil {
		return nil
	}
	prompt := exec.Command("osascript", "-e", fmt.Sprintf(`do shell script "%s" with administrator privileges`, escapeApple(shell)))
	out, err = prompt.CombinedOutput()
	if err != nil {
		if len(out) > 0 {
			return fmt.Errorf("%s", trim(string(out)))
		}
		return err
	}
	return nil
}

func repoDir() string {
	if dir, err := os.Getwd(); err == nil {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
	}
	return "."
}
