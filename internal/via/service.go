package via

import (
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"time"

	"github.com/hbar/hbar/internal/store"
)

const (
	// ProxyPort is the HTTP proxy added devices can use.
	ProxyPort = 10081
	dnsRelay  = 10053
)

// Service listens for traffic from devices a person added in the UI.
type Service struct {
	mu       sync.Mutex
	store    *store.Store
	dir      string
	upstream string
	proxyOn  bool
	dnsOn    bool
	detail   string
	proxyLn  net.Listener
	dnsLn    net.PacketConn
}

// Status is what the admin screen says about the path through Ano.
type Status struct {
	Proxy       bool
	DNS         bool
	DNSDetail   string
	Steer       bool
	SteerDetail string
}

// Start opens the proxy and tries to answer DNS.
func Start(st *store.Store, dir, upstream string) *Service {
	s := &Service{store: st, dir: dir, upstream: upstream, detail: "DNS port 53 is not open on this computer yet."}
	ln, err := net.Listen("tcp", fmt.Sprintf(":%d", ProxyPort))
	if err != nil {
		s.detail = "The Ano proxy could not listen. DNS port 53 is not open yet."
	} else {
		s.proxyLn = ln
		s.proxyOn = true
		s.detail = "DNS port 53 is not open yet. Allow DNS so a television can use Ano as its DNS server."
		go func() {
			_ = (&Proxy{Store: st}).Serve(ln)
		}()
	}
	return s
}

// Status reports whether devices can reach Ano.
func (s *Service) Status() Status {
	s.mu.Lock()
	defer s.mu.Unlock()
	return Status{
		Proxy: s.proxyOn, DNS: s.dnsOn, DNSDetail: s.detail,
		Steer: s.steerOn(), SteerDetail: s.steerDetail(),
	}
}

// EnableDNS asks this computer for permission to answer DNS on port 53.
func (s *Service) EnableDNS() error {
	if s.Status().DNS {
		return nil
	}
	if err := s.tryDNS(false); err == nil && s.Status().DNS {
		return nil
	}
	if err := s.tryDNS(true); err != nil {
		return err
	}
	if !s.Status().DNS {
		return fmt.Errorf("%s", s.Status().DNSDetail)
	}
	return nil
}

// Stop closes the listeners and asks the DNS relay to exit.
func (s *Service) Stop() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.proxyLn != nil {
		_ = s.proxyLn.Close()
	}
	if s.dnsLn != nil {
		_ = s.dnsLn.Close()
	}
	if s.dir != "" {
		_ = os.WriteFile(filepath.Join(s.dir, "via-dns.stop"), []byte("stop\n"), 0o644)
		_ = os.WriteFile(filepath.Join(s.dir, "via-steer.stop"), []byte("stop\n"), 0o644)
	}
}

func (s *Service) tryDNS(prompt bool) error {
	if s.Status().DNS {
		return nil
	}
	if pc, err := net.ListenPacket("udp", ":53"); err == nil {
		return s.serveDNS(pc, false, "Ano is answering DNS on port 53.")
	}
	s.mu.Lock()
	needRelay := s.dnsLn == nil
	s.mu.Unlock()
	if needRelay {
		pc, err := net.ListenPacket("udp", fmt.Sprintf("127.0.0.1:%d", dnsRelay))
		if err != nil {
			s.setDNS(false, "Ano could not open a DNS relay on this computer.")
			return err
		}
		go func() {
			_ = ServeDNS(pc, s.store, s.upstream, true)
		}()
		s.mu.Lock()
		s.dnsLn = pc
		s.mu.Unlock()
	}
	if err := s.relay(prompt); err != nil {
		s.setDNS(false, "Ano needs permission before it can answer DNS on port 53.")
		return err
	}
	ready := filepath.Join(s.dir, "via-dns.ready")
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(ready); err == nil {
			s.setDNS(true, "Ano is answering DNS on port 53.")
			return nil
		}
		time.Sleep(100 * time.Millisecond)
	}
	s.setDNS(false, "Ano could not take DNS port 53.")
	return fmt.Errorf("Ano could not take DNS port 53")
}

func (s *Service) serveDNS(pc net.PacketConn, prefixed bool, detail string) error {
	s.mu.Lock()
	s.dnsLn = pc
	s.dnsOn = true
	s.detail = detail
	s.mu.Unlock()
	go func() {
		_ = ServeDNS(pc, s.store, s.upstream, prefixed)
	}()
	return nil
}

func (s *Service) setDNS(on bool, detail string) {
	s.mu.Lock()
	s.dnsOn = on
	s.detail = detail
	s.mu.Unlock()
}

func (s *Service) relay(prompt bool) error {
	if err := os.MkdirAll(s.dir, 0o755); err != nil {
		return err
	}
	script := filepath.Join(s.dir, "via-dns.py")
	stop := filepath.Join(s.dir, "via-dns.stop")
	ready := filepath.Join(s.dir, "via-dns.ready")
	logPath := filepath.Join(s.dir, "via-dns.log")
	_ = os.Remove(stop)
	_ = os.Remove(ready)
	if err := os.WriteFile(script, []byte(dnsRelayScript), 0o755); err != nil {
		return err
	}
	shell := fmt.Sprintf("nohup /usr/bin/python3 %s %d %s %s >> %s 2>&1 &", quote(script), dnsRelay, quote(stop), quote(ready), quote(logPath))
	var cmd *exec.Cmd
	if prompt {
		cmd = exec.Command("osascript", "-e", fmt.Sprintf(`do shell script "%s" with administrator privileges`, escapeApple(shell)))
	} else {
		cmd = exec.Command("sudo", "-n", "/bin/sh", "-c", shell)
	}
	out, err := cmd.CombinedOutput()
	if err != nil {
		if len(out) > 0 {
			return fmt.Errorf("%s", trim(string(out)))
		}
		return err
	}
	return nil
}

func quote(path string) string {
	return "'" + path + "'"
}

func escapeApple(shell string) string {
	out := ""
	for _, r := range shell {
		if r == '\\' || r == '"' {
			out += "\\"
		}
		out += string(r)
	}
	return out
}

func trim(s string) string {
	if len(s) > 240 {
		return s[:240]
	}
	return s
}

const dnsRelayScript = `#!/usr/bin/env python3
import os, socket, sys
relay_port = int(sys.argv[1])
stop = sys.argv[2]
ready = sys.argv[3]
go = ("127.0.0.1", relay_port)
s = socket.socket(socket.AF_INET, socket.SOCK_DGRAM)
s.bind(("0.0.0.0", 53))
open(ready, "w").close()
s.settimeout(1.0)
while not os.path.exists(stop):
    try:
        data, addr = s.recvfrom(4096)
    except socket.timeout:
        continue
    try:
        ip = socket.inet_aton(addr[0])
    except OSError:
        continue
    port = int(addr[1]).to_bytes(2, "big")
    c = socket.socket(socket.AF_INET, socket.SOCK_DGRAM)
    c.settimeout(3)
    try:
        c.sendto(ip + port + data, go)
        reply, _ = c.recvfrom(8192)
    except OSError:
        c.close()
        continue
    c.close()
    if len(reply) > 6:
        try:
            s.sendto(reply[6:], addr)
        except OSError:
            pass
s.close()
`
