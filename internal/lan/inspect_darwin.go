//go:build darwin

package lan

import (
	"fmt"
	"os/exec"
	"strings"
)

func inspectHost() (Snapshot, error) {
	routeOut, err := exec.Command("route", "-n", "get", "default").CombinedOutput()
	if err != nil {
		return Snapshot{}, fmt.Errorf("could not read the home router: %s", strings.TrimSpace(string(routeOut)))
	}
	arpOut, err := exec.Command("arp", "-a").CombinedOutput()
	if err != nil {
		return Snapshot{}, fmt.Errorf("could not read devices on the home network: %s", strings.TrimSpace(string(arpOut)))
	}
	route := Parse(string(routeOut), "")
	links := []Link{{Iface: route.Iface, Router: route.Router, Room: "Ethernet"}}
	if wifi, ok := wifiLink(); ok && wifi.Iface != "" && wifi.Iface != route.Iface {
		links = append(links, wifi)
	} else if ok && wifi.Iface == route.Iface {
		links[0].Room = "Wi-Fi"
	}
	if route.Router == "" && (len(links) == 0 || links[0].Router == "") {
		return Snapshot{}, fmt.Errorf("this computer has no route to a home router")
	}
	neighbors := NeighborsOn(string(arpOut), links)
	neighbors = Merge(neighbors, discover(links))
	snap := Snapshot{Neighbors: neighbors}
	for _, link := range links {
		if link.Room == "Wi-Fi" && link.Router != "" {
			snap.Router = link.Router
			snap.Iface = "Wi-Fi"
		}
	}
	if snap.Router == "" {
		snap.Router = route.Router
		snap.Iface = route.Iface
	}
	return snap, nil
}
