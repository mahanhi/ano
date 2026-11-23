//go:build linux

package lan

import (
	"fmt"
	"net"
	"os"
	"os/exec"
	"strings"
)

func inspectHost() (Snapshot, error) {
	routeOut, err := exec.Command("ip", "route", "show", "default").CombinedOutput()
	if err != nil {
		return Snapshot{}, fmt.Errorf("could not read the home port: %s", strings.TrimSpace(string(routeOut)))
	}
	wan := ifaceFromDefaultRoute(string(routeOut))
	addrOut, err := exec.Command("ip", "-4", "-o", "addr", "show", "scope", "global").CombinedOutput()
	if err != nil {
		return Snapshot{}, fmt.Errorf("could not read the home port: %s", strings.TrimSpace(string(addrOut)))
	}
	iface, self := homePort(string(addrOut), wan)
	if iface == "" || net.ParseIP(self) == nil {
		return Snapshot{}, fmt.Errorf("the home port has no address")
	}
	neighOut, err := exec.Command("ip", "neigh", "show", "dev", iface).CombinedOutput()
	if err != nil {
		return Snapshot{}, fmt.Errorf("could not read devices on the home network: %s", strings.TrimSpace(string(neighOut)))
	}
	leases, _ := os.ReadFile("/var/lib/misc/dnsmasq.leases")
	neighbors := mergeLeaseNames(ParseNeigh(string(neighOut)), ParseLeases(string(leases)))
	var kept []Neighbor
	for _, neighbor := range neighbors {
		if neighbor.Addr == self {
			continue
		}
		kept = append(kept, neighbor)
	}
	return Snapshot{Router: self, Iface: iface, Neighbors: kept}, nil
}

func homePort(addrText, wan string) (iface, self string) {
	for _, line := range strings.Split(addrText, "\n") {
		fields := strings.Fields(line)
		if len(fields) < 4 || fields[2] != "inet" {
			continue
		}
		name := strings.TrimSuffix(fields[1], ":")
		if name == "" || name == wan || name == "lo" || virtualPort(name) {
			continue
		}
		ip, _, _ := strings.Cut(fields[3], "/")
		parsed := net.ParseIP(ip)
		if parsed == nil || !parsed.IsPrivate() {
			continue
		}
		return name, parsed.String()
	}
	return "", ""
}

func virtualPort(name string) bool {
	return name == "docker0" || strings.HasPrefix(name, "docker") || strings.HasPrefix(name, "veth") || strings.HasPrefix(name, "br-") || strings.HasPrefix(name, "virbr")
}
