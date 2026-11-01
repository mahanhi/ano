package lan

import (
	"bytes"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"
)

// Hint is a name a device announced for itself.
type Hint struct {
	Addr string
	MAC  string
	Name string
	Kind string
	Room string
}

var mdnsService = regexp.MustCompile(`(?i)\s(_[a-z0-9-]+\._tcp\.)\s+(.+)$`)

// standardServices are announcements used in any home when the network does not list its own.
var standardServices = []string{
	"_googlecast._tcp",
	"_airplay._tcp",
	"_raop._tcp",
	"_hap._tcp",
	"_ipp._tcp",
	"_printer._tcp",
	"_androidtvremote2._tcp",
}

// Merge applies announced names onto the neighbor list and fills a vendor name when a device stays unnamed.
func Merge(neighbors []Neighbor, hints []Hint) []Neighbor {
	for _, hint := range hints {
		idx := matchNeighbor(neighbors, hint)
		if idx < 0 {
			mac, ok := CanonicalMAC(hint.MAC)
			if !ok || hint.Addr == "" {
				continue
			}
			name := strings.TrimSpace(hint.Name)
			kind := hint.Kind
			if kind == "" {
				kind = KindOf(name)
			}
			room := hint.Room
			if room == "" {
				room = "Wi-Fi"
			}
			neighbors = append(neighbors, Neighbor{Name: name, Addr: hint.Addr, MAC: mac, Kind: kind, Room: room})
			continue
		}
		cur := neighbors[idx]
		if mac, ok := CanonicalMAC(hint.MAC); ok {
			cur.MAC = mac
		}
		if hint.Name != "" && better(cur.Name, hint.Name) {
			cur.Name = strings.TrimSpace(hint.Name)
		}
		nextKind := hint.Kind
		if hint.Name != "" {
			nextKind = preferKind(nextKind, KindOf(hint.Name))
		}
		cur.Kind = preferKind(cur.Kind, nextKind)
		if hint.Room != "" && hint.Room != "Wi-Fi" && hint.Room != "Ethernet" && hint.Room != "Home" {
			cur.Room = hint.Room
		}
		neighbors[idx] = cur
	}
	for i := range neighbors {
		name := strings.TrimSpace(neighbors[i].Name)
		if name == "" || name == "?" {
			if neighbors[i].Kind == "" {
				neighbors[i].Kind = "unknown"
			}
			continue
		}
		if neighbors[i].Kind == "" || neighbors[i].Kind == "unknown" {
			neighbors[i].Kind = KindOf(name)
		}
	}
	return neighbors
}

func discover(links []Link) []Hint {
	wifi := ""
	routers := map[string]bool{}
	for _, link := range links {
		routers[link.Router] = true
		if link.Room == "Wi-Fi" {
			wifi = link.Iface
		}
	}
	if wifi == "" && len(links) > 0 {
		wifi = links[0].Iface
	}
	var hints []Hint
	done := make(chan []Hint, 2)
	go func() { done <- ssdpHints(localIP(wifi), routers) }()
	go func() { done <- mdnsHints(wifi) }()
	hints = append(hints, <-done...)
	hints = append(hints, <-done...)
	return hints
}

func localIP(iface string) string {
	if iface == "" {
		return ""
	}
	out, err := exec.Command("ipconfig", "getifaddr", iface).CombinedOutput()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

func ssdpHints(local string, routers map[string]bool) []Hint {
	if local == "" {
		return nil
	}
	packets := ssdpSearch(local, 3*time.Second)
	seen := map[string]bool{}
	var hints []Hint
	client := &http.Client{Timeout: 1500 * time.Millisecond}
	for _, packet := range packets {
		loc := headerValue(packet, "LOCATION")
		if loc == "" || seen[loc] {
			continue
		}
		host := hostOf(loc)
		if host == "" || routers[host] || !privateIP(host) {
			continue
		}
		seen[loc] = true
		mac := rinconMAC(headerValue(packet, "USN"))
		body := fetchText(client, loc)
		friendly, model := xmlValue(body, "friendlyName"), xmlValue(body, "modelName")
		name := friendly
		if !useful(name) {
			name = model
		}
		kind := ServiceKind(xmlValue(body, "deviceType"))
		if kind == "" {
			kind = KindOf(name)
		}
		if kind == "unknown" {
			kind = KindOf(model)
		}
		hints = append(hints, Hint{Addr: host, MAC: mac, Name: name, Kind: kind})
	}
	return hints
}

func ssdpSearch(local string, wait time.Duration) []string {
	ip := net.ParseIP(local)
	conn, err := net.ListenUDP("udp4", &net.UDPAddr{IP: ip})
	if err != nil {
		return nil
	}
	defer conn.Close()
	msg := "M-SEARCH * HTTP/1.1\r\nHOST: 239.255.255.250:1900\r\nMAN: \"ssdp:discover\"\r\nMX: 1\r\nST: ssdp:all\r\n\r\n"
	_, _ = conn.WriteToUDP([]byte(msg), &net.UDPAddr{IP: net.ParseIP("239.255.255.250"), Port: 1900})
	_ = conn.SetReadDeadline(time.Now().Add(wait))
	var packets []string
	buf := make([]byte, 8192)
	for {
		n, _, err := conn.ReadFromUDP(buf)
		if err != nil {
			break
		}
		packets = append(packets, string(buf[:n]))
	}
	return packets
}

func mdnsHints(iface string) []Hint {
	if iface == "" {
		return nil
	}
	services := serviceTypes(iface)
	texts := make([]string, len(services))
	var wg sync.WaitGroup
	for i, service := range services {
		wg.Add(1)
		go func(i int, service string) {
			defer wg.Done()
			texts[i] = runDNS("-i", iface, "-B", service, "local.")
		}(i, service)
	}
	wg.Wait()
	type found struct{ service, name string }
	var founds []found
	seen := map[string]bool{}
	for _, text := range texts {
		for _, line := range strings.Split(text, "\n") {
			m := mdnsService.FindStringSubmatch(line)
			if len(m) != 3 {
				continue
			}
			name := strings.TrimSpace(m[2])
			key := m[1] + " " + name
			if name == "" || seen[key] {
				continue
			}
			seen[key] = true
			founds = append(founds, found{service: strings.TrimSuffix(m[1], "."), name: name})
		}
	}
	if len(founds) > 16 {
		founds = founds[:16]
	}
	got := make([][]Hint, len(founds))
	for i, item := range founds {
		wg.Add(1)
		go func(i int, item found) {
			defer wg.Done()
			text := runDNS("-i", iface, "-L", item.name, item.service, "local.")
			got[i] = hintsFromLookup(item.name, item.service, text)
		}(i, item)
	}
	wg.Wait()
	var hints []Hint
	for _, batch := range got {
		hints = append(hints, batch...)
	}
	return hints
}

func serviceTypes(iface string) []string {
	text := runDNS("-i", iface, "-B", "_services._dns-sd._udp", "local.")
	found := map[string]bool{}
	for _, field := range strings.Fields(text) {
		field = strings.Trim(field, ".")
		if !strings.HasPrefix(field, "_") {
			continue
		}
		if !strings.Contains(field, "._tcp") && !strings.Contains(field, "._udp") {
			continue
		}
		if strings.Contains(field, "_dns-sd") || strings.Contains(field, "_services") {
			continue
		}
		found[field] = true
	}
	types := make([]string, 0, len(found))
	for service := range found {
		types = append(types, service)
	}
	if len(types) == 0 {
		return append([]string(nil), standardServices...)
	}
	sort.Slice(types, func(i, j int) bool {
		if serviceRank(types[i]) != serviceRank(types[j]) {
			return serviceRank(types[i]) < serviceRank(types[j])
		}
		return types[i] < types[j]
	})
	if len(types) > 12 {
		types = types[:12]
	}
	return types
}

func serviceRank(service string) int {
	s := strings.ToLower(service)
	switch {
	case strings.Contains(s, "_googlecast"), strings.Contains(s, "_androidtvremote"), strings.Contains(s, "_hap"), strings.Contains(s, "_ipp"), strings.Contains(s, "_printer"), strings.Contains(s, "_raop"), strings.Contains(s, "_airplay"):
		return 0
	default:
		return 1
	}
}

func hintsFromLookup(instance, service, text string) []Hint {
	instance = unescapeMDNS(instance)
	fields := map[string]string{}
	for _, line := range strings.Split(text, "\n") {
		for _, part := range strings.Fields(strings.ReplaceAll(line, `\ `, "\x00")) {
			part = strings.ReplaceAll(part, "\x00", " ")
			key, val, ok := strings.Cut(part, "=")
			if ok && !strings.Contains(key, "/") {
				fields[strings.ToLower(key)] = unescapeMDNS(val)
			}
		}
	}
	addr := ""
	if i := strings.Index(text, "reached at "); i >= 0 {
		host := text[i+len("reached at "):]
		host, _, _ = strings.Cut(host, ":")
		host = strings.TrimSpace(host)
		if ip := dashedIP(host); ip != "" {
			addr = ip
		}
	}
	mac := fields["deviceid"]
	if mac == "" {
		mac = fields["bt"]
	}
	name := fields["n"]
	if name == "" {
		name = fields["fn"]
	}
	if name == "" {
		name = fields["md"]
	}
	if name == "" {
		name = instance
	}
	if name == "" && addr == "" && mac == "" {
		return nil
	}
	kind := HomeKitKind(fields["ci"])
	if kind == "" {
		kind = ServiceKind(service)
	}
	if kind == "" {
		kind = KindOf(name)
	}
	return []Hint{{Addr: addr, MAC: mac, Name: name, Kind: kind}}
}

func runDNS(args ...string) string {
	cmd := exec.Command("dns-sd", args...)
	var buf bytes.Buffer
	cmd.Stdout = &buf
	cmd.Stderr = &buf
	if err := cmd.Start(); err != nil {
		return ""
	}
	done := make(chan struct{})
	go func() {
		_ = cmd.Wait()
		close(done)
	}()
	select {
	case <-time.After(2200 * time.Millisecond):
		_ = cmd.Process.Signal(os.Interrupt)
		select {
		case <-done:
		case <-time.After(400 * time.Millisecond):
			_ = cmd.Process.Kill()
			<-done
		}
	case <-done:
	}
	return buf.String()
}

func matchNeighbor(neighbors []Neighbor, hint Hint) int {
	mac, macOK := CanonicalMAC(hint.MAC)
	for i, n := range neighbors {
		if hint.Addr != "" && n.Addr == hint.Addr {
			return i
		}
		if macOK && nearMAC(n.MAC, mac) {
			return i
		}
	}
	return -1
}

func nearMAC(a, b string) bool {
	if a == b {
		return true
	}
	aa := strings.Split(a, ":")
	bb := strings.Split(b, ":")
	if len(aa) != 6 || len(bb) != 6 {
		return false
	}
	for i := 0; i < 5; i++ {
		if aa[i] != bb[i] {
			return false
		}
	}
	return true
}

func better(cur, next string) bool {
	next = strings.TrimSpace(next)
	if next == "" || next == "?" {
		return false
	}
	cur = strings.TrimSpace(cur)
	if cur == "" || cur == "?" || strings.HasPrefix(cur, "Device ") || strings.HasSuffix(cur, " device") || strings.HasSuffix(cur, " light") || strings.HasSuffix(cur, " printer") || strings.HasSuffix(cur, " speaker") {
		return true
	}
	return specific(KindOf(next)) && !specific(KindOf(cur))
}

func specific(kind string) bool {
	switch kind {
	case "tv", "camera", "garage", "speaker", "light", "printer", "phone", "thermostat":
		return true
	default:
		return false
	}
}

func preferKind(cur, next string) string {
	if next == "" || next == "unknown" {
		if cur != "" {
			return cur
		}
		return "unknown"
	}
	if cur == "" || cur == "unknown" || cur == "computer" {
		return next
	}
	if next == "computer" {
		return cur
	}
	return cur
}

func headerValue(packet, name string) string {
	prefix := strings.ToLower(name) + ":"
	for _, line := range strings.Split(packet, "\n") {
		if strings.HasPrefix(strings.ToLower(strings.TrimSpace(line)), prefix) {
			return strings.TrimSpace(line[len(prefix):])
		}
	}
	return ""
}

func hostOf(raw string) string {
	raw = strings.TrimSpace(raw)
	raw = strings.TrimPrefix(raw, "http://")
	raw = strings.TrimPrefix(raw, "https://")
	host, _, _ := strings.Cut(raw, "/")
	host, _, _ = strings.Cut(host, ":")
	return host
}

func privateIP(ip string) bool {
	parsed := net.ParseIP(ip)
	if parsed == nil {
		return false
	}
	return parsed.IsPrivate()
}

func rinconMAC(usn string) string {
	up := strings.ToUpper(usn)
	const mark = "RINCON_"
	i := strings.Index(up, mark)
	if i < 0 || len(up) < i+len(mark)+12 {
		return ""
	}
	hex := up[i+len(mark) : i+len(mark)+12]
	if len(hex) != 12 {
		return ""
	}
	parts := make([]string, 6)
	for n := 0; n < 6; n++ {
		parts[n] = strings.ToLower(hex[n*2 : n*2+2])
	}
	mac := strings.Join(parts, ":")
	if _, ok := CanonicalMAC(mac); !ok {
		return ""
	}
	return mac
}

func xmlValue(body, tag string) string {
	open := "<" + tag + ">"
	i := strings.Index(body, open)
	if i < 0 {
		return ""
	}
	rest := body[i+len(open):]
	j := strings.Index(rest, "</")
	if j < 0 {
		return ""
	}
	return strings.TrimSpace(rest[:j])
}

func fetchText(client *http.Client, raw string) string {
	if !strings.HasPrefix(raw, "http://") {
		return ""
	}
	resp, err := client.Get(raw)
	if err != nil {
		return ""
	}
	defer resp.Body.Close()
	buf, err := io.ReadAll(io.LimitReader(resp.Body, 64*1024))
	if err != nil {
		return ""
	}
	return string(buf)
}

func useful(name string) bool {
	name = strings.TrimSpace(name)
	if name == "" || strings.HasPrefix(name, "192.168.") || strings.HasPrefix(name, "10.") {
		return false
	}
	return true
}

func dashedIP(host string) string {
	host = strings.Trim(host, ".")
	host = strings.TrimSuffix(host, ".local")
	parts := strings.Split(host, "-")
	if len(parts) != 4 {
		return ""
	}
	ip := strings.Join(parts, ".")
	if net.ParseIP(ip) == nil {
		return ""
	}
	return ip
}

func unescapeMDNS(s string) string {
	s = strings.ReplaceAll(s, `\032`, " ")
	s = strings.ReplaceAll(s, `\ `, " ")
	s = strings.ReplaceAll(s, `\'`, "'")
	s = strings.ReplaceAll(s, `\\`, `\`)
	return s
}
