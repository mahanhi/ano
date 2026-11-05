package via

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"net"

	"github.com/hbar/hbar/internal/lan"
	"github.com/hbar/hbar/internal/store"
)

// SteerDevice is one added device whose outgoing packets Ano is recording.
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

// EnableSteer stays off. Claiming the router and forwarding those packets
// through this Mac panicked the kernel and restarted it.
func (s *Service) EnableSteer() (string, error) {
	return "", fmt.Errorf("this path stays off because it restarted the Mac")
}

// DisableSteer sends the added devices back to the router.
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

// RefreshSteer rewrites the added-device list while the path is on.
func (s *Service) RefreshSteer() {
	if !s.steerOn() {
		return
	}
	nic, err := lan.WifiNIC()
	if err != nil {
		return
	}
	targets, err := addedDevices(s.store, nic.Addr, nic.Gateway)
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
		return "Added devices reach the network through this computer. Outgoing packets are recorded here, then forwarded to the router. Streaming stays direct. Stop returns them to the router."
	}
	return "Outgoing packets stay on the router. Moving them through this Mac restarted it, so that path stays off."
}

func addedDevices(st *store.Store, skip ...string) ([]SteerDevice, error) {
	rows, err := st.ViaTargets()
	if err != nil {
		return nil, err
	}
	omit := map[string]bool{}
	for _, addr := range skip {
		omit[addr] = true
	}
	var out []SteerDevice
	seen := map[string]bool{}
	for _, row := range rows {
		if omit[row.Addr] || seen[row.Addr] {
			continue
		}
		mac, ok := lan.CanonicalMAC(row.MAC)
		if !ok || net.ParseIP(row.Addr) == nil {
			continue
		}
		seen[row.Addr] = true
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
