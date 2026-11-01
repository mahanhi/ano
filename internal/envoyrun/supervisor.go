package envoyrun

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"time"

	"github.com/hbar/hbar/internal/envoycfg"
	"github.com/hbar/hbar/internal/store"
)

// Supervisor writes the Envoy config from approved rules and keeps the process up.
type Supervisor struct {
	mu      sync.Mutex
	bin     string
	cfgPath string
	logPath string
	cmd     *exec.Cmd
	done    chan struct{}
	store   *store.Store
	message string
}

// Start launches Envoy when a binary is available.
func Start(st *store.Store, dataDir string) *Supervisor {
	s := &Supervisor{
		bin:     findBin(),
		cfgPath: filepath.Join(dataDir, "envoy.yaml"),
		logPath: filepath.Join(dataDir, "envoy-access.log"),
		store:   st,
	}
	if err := s.reload(); err != nil {
		s.message = err.Error()
	}
	return s
}

// Reload regenerates the config and restarts Envoy.
func (s *Supervisor) Reload() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.reload()
}

func (s *Supervisor) reload() error {
	routes, err := s.store.EnvoyRoutes()
	if err != nil {
		return err
	}
	cfgRoutes := make([]envoycfg.Route, 0, len(routes))
	for _, r := range routes {
		cfgRoutes = append(cfgRoutes, envoycfg.Route{Destination: r.Destination, Action: r.Action, Reroute: r.Reroute})
	}
	body := envoycfg.Render(envoycfg.Options{
		ListenPort: 10080,
		AdminPort:  19001,
		AccessLog:  s.logPath,
	}, cfgRoutes)
	if err := os.WriteFile(s.cfgPath, []byte(body), 0o644); err != nil {
		return err
	}
	s.stopLocked()
	if s.bin == "" {
		s.message = "Envoy binary not found. Install it and set HBAR_ENVOY_BIN."
		return nil
	}
	cmd := exec.Command(s.bin, "-c", s.cfgPath, "--log-level", "warning")
	cmd.Stdout = io.Discard
	cmd.Stderr = io.Discard
	if err := cmd.Start(); err != nil {
		s.message = err.Error()
		return err
	}
	done := make(chan struct{})
	s.cmd = cmd
	s.done = done
	s.message = ""
	go func() {
		_ = cmd.Wait()
		close(done)
	}()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		resp, err := http.Get("http://127.0.0.1:19001/ready")
		if err == nil {
			resp.Body.Close()
			if resp.StatusCode == 200 {
				return nil
			}
		}
		time.Sleep(100 * time.Millisecond)
	}
	s.message = "Envoy did not become ready"
	return fmt.Errorf("%s", s.message)
}

// Status is what the dashboard shows.
func (s *Supervisor) Status() map[string]any {
	s.mu.Lock()
	defer s.mu.Unlock()
	running := false
	resp, err := http.Get("http://127.0.0.1:19001/ready")
	if err == nil {
		resp.Body.Close()
		running = resp.StatusCode == 200
	}
	routes, _ := s.store.EnvoyRoutes()
	version := ""
	if s.bin != "" {
		out, err := exec.Command(s.bin, "--version").Output()
		if err == nil {
			version = string(out)
		}
	}
	return map[string]any{
		"running":    running,
		"binary":     s.bin,
		"configPath": s.cfgPath,
		"listen":     "127.0.0.1:10080",
		"routes":     len(routes),
		"message":    s.message,
		"version":    version,
	}
}

// Stop terminates Envoy.
func (s *Supervisor) Stop() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.stopLocked()
}

func (s *Supervisor) stopLocked() {
	if s.cmd == nil || s.cmd.Process == nil {
		return
	}
	_ = s.cmd.Process.Kill()
	if s.done != nil {
		select {
		case <-s.done:
		case <-time.After(2 * time.Second):
		}
	}
	s.cmd = nil
	s.done = nil
}

func findBin() string {
	if p := os.Getenv("HBAR_ENVOY_BIN"); p != "" {
		return p
	}
	for _, p := range []string{"/usr/local/bin/envoy", "/workspace/bin/envoy"} {
		if st, err := os.Stat(p); err == nil && !st.IsDir() {
			return p
		}
	}
	if p, err := exec.LookPath("envoy"); err == nil {
		return p
	}
	return ""
}
