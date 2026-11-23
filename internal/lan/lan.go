// Package lan reads the home router and the devices this computer can see on that network.
package lan

import (
	"os/exec"
	"regexp"
	"strconv"
	"strings"
)

// Neighbor is one device the home router has passed to this computer.
type Neighbor struct {
	Name string
	Addr string
	MAC  string
	Kind string
	Room string
}

// Link is one network this computer is on.
type Link struct {
	Iface  string
	Router string
	Room   string
}

// Snapshot is the router address and the devices on its network.
type Snapshot struct {
	Router    string
	Iface     string
	Neighbors []Neighbor
}

var (
	arpLine = regexp.MustCompile(`(?i)^(\S+)\s+\(([0-9.]+)\)\s+at\s+([0-9a-f:]+)\s+`)
	gateway = regexp.MustCompile(`(?m)^\s*gateway:\s+(\S+)`)
	ifaceRE = regexp.MustCompile(`(?m)^\s*interface:\s+(\S+)`)
)

// Inspect asks the operating system for the home port and the devices on it.
func Inspect() (Snapshot, error) {
	return inspectHost()
}

// Parse splits route and arp text into a router and the devices on that link.
func Parse(routeText, arpText string) Snapshot {
	router := ""
	if m := gateway.FindStringSubmatch(routeText); len(m) == 2 {
		router = m[1]
	}
	link := ""
	if m := ifaceRE.FindStringSubmatch(routeText); len(m) == 2 {
		link = m[1]
	}
	var neighbors []Neighbor
	seen := map[string]bool{}
	for _, line := range strings.Split(arpText, "\n") {
		line = strings.TrimSpace(line)
		lower := strings.ToLower(line)
		if strings.Contains(lower, "incomplete") || strings.Contains(lower, "permanent") {
			continue
		}
		if link != "" && !strings.Contains(lower, " on "+strings.ToLower(link)+" ") {
			continue
		}
		m := arpLine.FindStringSubmatch(line)
		if len(m) != 4 {
			continue
		}
		name, addr := m[1], m[2]
		mac, ok := CanonicalMAC(m[3])
		if !ok || addr == router || skipAddr(addr) || mac == "ff:ff:ff:ff:ff:ff" {
			continue
		}
		if seen[mac] {
			continue
		}
		seen[mac] = true
		if name == "?" {
			name = ""
		}
		neighbors = append(neighbors, Neighbor{Name: name, Addr: addr, MAC: mac, Kind: KindOf(name)})
	}
	return Snapshot{Router: router, Iface: link, Neighbors: neighbors}
}

// CanonicalMAC pads each octet so the same device keeps one identity.
func CanonicalMAC(mac string) (string, bool) {
	parts := strings.Split(strings.ToLower(strings.TrimSpace(mac)), ":")
	if len(parts) != 6 {
		return "", false
	}
	for i, part := range parts {
		if part == "" || len(part) > 2 {
			return "", false
		}
		if len(part) == 1 {
			parts[i] = "0" + part
		}
		for _, c := range parts[i] {
			if (c < '0' || c > '9') && (c < 'a' || c > 'f') {
				return "", false
			}
		}
	}
	return strings.Join(parts, ":"), true
}

func skipAddr(addr string) bool {
	parts := strings.Split(addr, ".")
	if len(parts) != 4 {
		return true
	}
	first, err := strconv.Atoi(parts[0])
	if err != nil {
		return true
	}
	return first >= 224
}

// ParseNeigh reads `ip neigh` lines that have a hardware address.
func ParseNeigh(text string) []Neighbor {
	var out []Neighbor
	seen := map[string]bool{}
	for _, line := range strings.Split(text, "\n") {
		fields := strings.Fields(line)
		if len(fields) < 6 || fields[3] != "lladdr" {
			continue
		}
		addr := fields[0]
		mac, ok := CanonicalMAC(fields[4])
		if !ok || skipAddr(addr) || seen[mac] {
			continue
		}
		state := strings.ToUpper(fields[len(fields)-1])
		if state == "FAILED" || state == "INCOMPLETE" {
			continue
		}
		seen[mac] = true
		out = append(out, Neighbor{Addr: addr, MAC: mac, Kind: "unknown", Room: "Home"})
	}
	return out
}

// ParseLeases reads a dnsmasq lease file. A star hostname is left blank.
func ParseLeases(text string) []Neighbor {
	var out []Neighbor
	seen := map[string]bool{}
	for _, line := range strings.Split(text, "\n") {
		fields := strings.Fields(line)
		if len(fields) < 4 {
			continue
		}
		mac, ok := CanonicalMAC(fields[1])
		addr := fields[2]
		if !ok || skipAddr(addr) || seen[mac] {
			continue
		}
		seen[mac] = true
		name := fields[3]
		if name == "*" {
			name = ""
		}
		out = append(out, Neighbor{Name: name, Addr: addr, MAC: mac, Kind: KindOf(name), Room: "Home"})
	}
	return out
}

func mergeLeaseNames(neighbors, leases []Neighbor) []Neighbor {
	byMAC := map[string]int{}
	byAddr := map[string]int{}
	for i, neighbor := range neighbors {
		byMAC[neighbor.MAC] = i
		byAddr[neighbor.Addr] = i
	}
	for _, lease := range leases {
		if i, ok := byMAC[lease.MAC]; ok {
			if lease.Name != "" {
				neighbors[i].Name = lease.Name
				neighbors[i].Kind = preferKind(neighbors[i].Kind, KindOf(lease.Name))
			}
			continue
		}
		if _, ok := byAddr[lease.Addr]; ok {
			continue
		}
		neighbors = append(neighbors, lease)
	}
	return neighbors
}

// NeighborsOn keeps devices on the given links and skips each link's router.
func NeighborsOn(arpText string, links []Link) []Neighbor {
	allowed := map[string]Link{}
	routers := map[string]bool{}
	for _, link := range links {
		if link.Iface == "" {
			continue
		}
		allowed[strings.ToLower(link.Iface)] = link
		if link.Router != "" {
			routers[link.Router] = true
		}
	}
	var neighbors []Neighbor
	seen := map[string]bool{}
	for _, line := range strings.Split(arpText, "\n") {
		line = strings.TrimSpace(line)
		lower := strings.ToLower(line)
		if strings.Contains(lower, "incomplete") || strings.Contains(lower, "permanent") {
			continue
		}
		iface := ifaceOf(lower)
		link, ok := allowed[iface]
		if !ok {
			continue
		}
		m := arpLine.FindStringSubmatch(line)
		if len(m) != 4 {
			continue
		}
		name, addr := m[1], m[2]
		mac, good := CanonicalMAC(m[3])
		if !good || routers[addr] || skipAddr(addr) || mac == "ff:ff:ff:ff:ff:ff" {
			continue
		}
		if seen[addr] {
			continue
		}
		seen[addr] = true
		if name == "?" {
			name = ""
		}
		room := link.Room
		if room == "" {
			room = "Home"
		}
		neighbors = append(neighbors, Neighbor{Name: name, Addr: addr, MAC: mac, Kind: KindOf(name), Room: room})
	}
	return neighbors
}

func ifaceOf(lowerLine string) string {
	const marker = " on "
	i := strings.Index(lowerLine, marker)
	if i < 0 {
		return ""
	}
	rest := strings.TrimSpace(lowerLine[i+len(marker):])
	field, _, _ := strings.Cut(rest, " ")
	return field
}

func wifiLink() (Link, bool) {
	out, err := exec.Command("networksetup", "-listallhardwareports").CombinedOutput()
	if err != nil {
		return Link{}, false
	}
	iface := wifiDevice(string(out))
	if iface == "" {
		return Link{}, false
	}
	routerOut, err := exec.Command("ipconfig", "getoption", iface, "router").CombinedOutput()
	router := strings.TrimSpace(string(routerOut))
	if err != nil || router == "" || strings.Contains(router, " ") {
		return Link{}, false
	}
	return Link{Iface: iface, Router: router, Room: "Wi-Fi"}, true
}

func wifiDevice(text string) string {
	blocks := strings.Split(text, "Hardware Port:")
	for _, block := range blocks {
		if !strings.Contains(block, "Wi-Fi") {
			continue
		}
		for _, line := range strings.Split(block, "\n") {
			line = strings.TrimSpace(line)
			if strings.HasPrefix(line, "Device: ") {
				return strings.TrimPrefix(line, "Device: ")
			}
		}
	}
	return ""
}

// KindOf groups a device by role words in the name it announced.
// A name that does not say what the device is stays unknown.
func KindOf(name string) string {
	n := strings.ToLower(name)
	for _, sep := range []string{"-", "_", ".", "/"} {
		n = strings.ReplaceAll(n, sep, " ")
	}
	if strings.TrimSpace(n) == "" || strings.TrimSpace(n) == "?" {
		return "unknown"
	}
	fields := strings.Fields(n)
	has := func(words ...string) bool {
		for _, field := range fields {
			for _, word := range words {
				if field == word {
					return true
				}
			}
		}
		return false
	}
	switch {
	case has("speaker", "speakers", "soundbar"):
		return "speaker"
	case has("camera", "cameras", "doorbell"):
		return "camera"
	case has("garage"):
		return "garage"
	case has("light", "lights", "bulb", "bulbs", "lamp", "lamps"):
		return "light"
	case has("printer", "printers"):
		return "printer"
	case has("phone", "phones"):
		return "phone"
	case has("television", "televisions"):
		return "tv"
	case has("tv", "tvs") && !strings.Contains(n, "tv room"):
		return "tv"
	case has("thermostat", "thermostats"):
		return "thermostat"
	default:
		return "unknown"
	}
}

// HomeKitKind maps a HomeKit category number. The numbers are the same in every home.
func HomeKitKind(ci string) string {
	switch strings.TrimSpace(ci) {
	case "4":
		return "garage"
	case "5":
		return "light"
	case "9":
		return "thermostat"
	case "17", "18":
		return "camera"
	case "31":
		return "tv"
	default:
		return ""
	}
}

// ServiceKind maps a discovery service to a role. Cast and AirPlay stay unset,
// because those services are used by more than one kind of device.
func ServiceKind(service string) string {
	s := strings.ToLower(service)
	switch {
	case strings.Contains(s, "_ipp"), strings.Contains(s, "_printer"), strings.Contains(s, "_pdl-datastream"), strings.Contains(s, "device:printer"):
		return "printer"
	case strings.Contains(s, "_raop"):
		return "speaker"
	case strings.Contains(s, "_androidtvremote"):
		return "tv"
	default:
		return ""
	}
}
