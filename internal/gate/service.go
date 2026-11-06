package gate

import (
	"fmt"
	"net"
	"strings"
	"sync"
	"time"

	"github.com/ano/ano/internal/store"
)

// Service watches forwarded internet conversations while Ano is the gateway.
type Service struct {
	mu     sync.Mutex
	store  *store.Store
	dir    string
	on     bool
	detail string
	plan   Plan
	prev   map[string]int
	saved  string
}

// Status is what the screen reads about the two ports.
type Status struct {
	On       bool   `json:"on"`
	Ready    bool   `json:"ready"`
	CanStart bool   `json:"canStart"`
	Detail   string `json:"detail"`
	WAN      string `json:"wan"`
	WANAddr  string `json:"wanAddr"`
	LAN      string `json:"lan"`
	LANAddr  string `json:"lanAddr"`
	LANNet   string `json:"lanNet"`
}

// Start prepares the gateway. It does not turn forwarding on.
// On Ano it also records the screen name in /etc/hosts.
func Start(st *store.Store, dir string) *Service {
	publishScreen()
	return &Service{store: st, dir: dir, detail: "Ano is not the gateway yet."}
}

// Status reports the modem port, the home port, and whether forwarding is on.
func (s *Service) Status() Status {
	s.mu.Lock()
	defer s.mu.Unlock()
	plan, err := inspect()
	if s.on && err == nil {
		plan = s.plan
	}
	ready := err == nil
	detail := s.detail
	if !s.on && err != nil {
		detail = err.Error()
	} else if !s.on && ready {
		detail = fmt.Sprintf("Router-side port %s is %s. Access-point port %s is %s on %s. A device is recorded after it uses %s as its gateway.",
			plan.WAN, plan.WANAddr, plan.LAN, plan.LANAddr, plan.LANNet, plan.LANAddr)
		if !canApply() {
			detail += " The gateway starts on Ano."
		}
	}
	return Status{On: s.on, Ready: ready, CanStart: canApply() && ready && !s.on, Detail: detail, WAN: plan.WAN, WANAddr: plan.WANAddr, LAN: plan.LAN, LANAddr: plan.LANAddr, LANNet: plan.LANNet}
}

// Collect records new forwarded bytes. It does nothing until the gateway is on.
func (s *Service) Collect() error {
	s.mu.Lock()
	if !s.on {
		s.mu.Unlock()
		return nil
	}
	plan := s.plan
	if s.prev == nil {
		s.prev = map[string]int{}
	}
	s.mu.Unlock()
	text, err := readForwarded()
	if err != nil {
		return nil
	}
	next := map[string]int{}
	for _, flow := range parseForwarded(text, plan.LANNet) {
		key := flow.Proto + " " + flow.Src + " " + flow.Dst + " " + fmt.Sprint(flow.Port)
		next[key] = flow.Bytes
		s.mu.Lock()
		old := s.prev[key]
		s.mu.Unlock()
		delta := flow.Bytes - old
		if delta < 0 {
			delta = flow.Bytes
		}
		if delta <= 0 {
			continue
		}
		_ = s.store.NoteForward(store.ForwardNote{Addr: flow.Src, Remote: flow.Dst, Port: flow.Port, Protocol: flow.Proto, Bytes: delta})
	}
	s.mu.Lock()
	s.prev = next
	s.mu.Unlock()
	return nil
}

// Loop collects while the process is up.
func (s *Service) Loop() {
	tick := time.NewTicker(2 * time.Second)
	names := time.NewTicker(10 * time.Minute)
	defer tick.Stop()
	defer names.Stop()
	for {
		select {
		case <-tick.C:
			_ = s.Collect()
		case <-names.C:
			_ = s.Refresh()
		}
	}
}

func targets(st *store.Store) (ips, names []string, err error) {
	routes, err := st.EnvoyRoutes()
	if err != nil {
		return nil, nil, err
	}
	seenIP := map[string]bool{}
	seenName := map[string]bool{}
	for _, route := range routes {
		if route.Action != "drop" {
			continue
		}
		dest := strings.TrimSpace(route.Destination)
		if host, _, splitErr := splitHost(dest); splitErr == nil {
			dest = host
		}
		if ip := net.ParseIP(dest); ip != nil && ip.To4() != nil {
			if !seenIP[ip.String()] {
				seenIP[ip.String()] = true
				ips = append(ips, ip.String())
			}
			continue
		}
		name := strings.TrimSuffix(strings.ToLower(dest), ".")
		if safeName(name) && !seenName[name] {
			seenName[name] = true
			names = append(names, name)
		}
	}
	return ips, names, nil
}

func splitHost(dest string) (string, string, error) {
	if !strings.Contains(dest, ":") {
		return dest, "", fmt.Errorf("no port")
	}
	return net.SplitHostPort(dest)
}
