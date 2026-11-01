// Package flow reads byte counts for conversations this computer has with other devices.
package flow

import (
	"net"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
)

// Flow is one connection and the bytes counted on it so far.
type Flow struct {
	Local    string
	Remote   string
	Port     int
	Protocol string
	Service  string
	Bytes    int
}

// Reading is the new bytes for one device and one kind of talk.
type Reading struct {
	Addr     string
	Service  string
	Protocol string
	Delta    int
	Total    int
}

// Meter remembers the last count for each connection so a later read reports only the new bytes.
type Meter struct {
	prev map[string]int
}

var flowLine = regexp.MustCompile(`(?i)^(tcp|udp)\S*\s+\d+\s+\d+\s+(\S+)\s+(\S+)\s+(\S+)\s+(\d+)\s+(\d+)`)

// Read asks the operating system for current connection byte counts.
func Read() ([]Flow, error) {
	out, err := exec.Command("netstat", "-anv", "-f", "inet").CombinedOutput()
	if err != nil {
		return nil, err
	}
	return Parse(string(out)), nil
}

// Parse extracts conversations from netstat text.
func Parse(text string) []Flow {
	var flows []Flow
	for _, line := range strings.Split(text, "\n") {
		m := flowLine.FindStringSubmatch(strings.TrimSpace(line))
		if len(m) != 7 {
			continue
		}
		localHost, localPort, ok := endpoint(m[2])
		if !ok {
			continue
		}
		remoteHost, remotePort, ok := endpoint(m[3])
		if !ok || remoteHost == localHost {
			continue
		}
		rx, _ := strconv.Atoi(m[5])
		tx, _ := strconv.Atoi(m[6])
		if rx+tx <= 0 {
			continue
		}
		port, service := serviceOf(localPort, remotePort)
		if service == "" {
			continue
		}
		proto := "TCP"
		if strings.HasPrefix(strings.ToLower(m[1]), "udp") {
			proto = "UDP"
		}
		flows = append(flows, Flow{Local: localHost, Remote: remoteHost, Port: port, Protocol: proto, Service: service, Bytes: rx + tx})
	}
	return flows
}

// Readings turns absolute counts into the bytes added since the previous read.
func (m *Meter) Readings(flows []Flow) []Reading {
	if m.prev == nil {
		m.prev = map[string]int{}
	}
	type agg struct {
		proto string
		delta int
		total int
	}
	grouped := map[string]*agg{}
	seen := map[string]int{}
	for _, flow := range flows {
		key := flow.Protocol + " " + flow.Local + " " + flow.Remote + " " + strconv.Itoa(flow.Port)
		delta := flow.Bytes
		if old, ok := m.prev[key]; ok {
			delta = flow.Bytes - old
			if delta < 0 {
				delta = flow.Bytes
			}
		}
		seen[key] = flow.Bytes
		id := flow.Remote + "\t" + flow.Service
		row := grouped[id]
		if row == nil {
			row = &agg{proto: flow.Protocol}
			grouped[id] = row
		}
		row.delta += delta
		row.total += flow.Bytes
	}
	m.prev = seen
	var out []Reading
	for id, row := range grouped {
		remote, service, _ := strings.Cut(id, "\t")
		if row.total <= 0 {
			continue
		}
		out = append(out, Reading{Addr: remote, Service: service, Protocol: row.proto, Delta: row.delta, Total: row.total})
	}
	return out
}

func endpoint(raw string) (string, int, bool) {
	dot := strings.LastIndex(raw, ".")
	if dot <= 0 {
		return "", 0, false
	}
	host := raw[:dot]
	if net.ParseIP(host) == nil {
		return "", 0, false
	}
	port, err := strconv.Atoi(raw[dot+1:])
	if err != nil {
		return "", 0, false
	}
	return host, port, true
}

func serviceOf(localPort, remotePort int) (int, string) {
	if name, ok := known(remotePort); ok {
		return remotePort, name
	}
	if name, ok := known(localPort); ok {
		return localPort, name
	}
	return 0, ""
}

func known(port int) (string, bool) {
	switch port {
	case 8008, 8009:
		return "Cast", true
	case 443, 8443:
		return "HTTPS", true
	case 80, 8080:
		return "HTTP", true
	case 1400:
		return "Sonos", true
	case 7000:
		return "AirPlay", true
	case 5353, 1900, 137, 138, 139, 67, 68, 123:
		return "", false
	default:
		return "", false
	}
}
