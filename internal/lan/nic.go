package lan

import (
	"fmt"
	"math/bits"
	"net"
	"os/exec"
	"strconv"
	"strings"
)

// NIC is this computer's Wi-Fi address and the router on that network.
type NIC struct {
	Iface      string
	Addr       string
	MAC        string
	Gateway    string
	GatewayMAC string
	LAN        string
}

// WifiNIC reads the Wi-Fi interface Ano can use for an added Fire TV.
func WifiNIC() (NIC, error) {
	link, ok := wifiLink()
	if !ok {
		return NIC{}, fmt.Errorf("Wi-Fi is not connected")
	}
	out, err := exec.Command("ifconfig", link.Iface).CombinedOutput()
	if err != nil {
		return NIC{}, fmt.Errorf("could not read Wi-Fi")
	}
	arpOut, _ := exec.Command("arp", "-n", link.Router).CombinedOutput()
	return ParseWifi(string(out), string(arpOut), link.Iface, link.Router)
}

// ParseWifi pulls the address, mask, and router MAC out of system text.
func ParseWifi(ifconfig, arpText, iface, gateway string) (NIC, error) {
	nic := NIC{Iface: iface, Gateway: gateway}
	for _, line := range strings.Split(ifconfig, "\n") {
		fields := strings.Fields(strings.TrimSpace(line))
		if len(fields) >= 2 && fields[0] == "ether" {
			mac, ok := CanonicalMAC(fields[1])
			if ok {
				nic.MAC = mac
			}
		}
		if len(fields) >= 4 && fields[0] == "inet" && fields[2] == "netmask" {
			nic.Addr = fields[1]
			ones, err := maskOnes(fields[3])
			if err != nil {
				return NIC{}, err
			}
			ip := net.ParseIP(fields[1]).To4()
			if ip == nil {
				return NIC{}, fmt.Errorf("Wi-Fi address %s", fields[1])
			}
			network := ip.Mask(net.CIDRMask(ones, 32))
			nic.LAN = fmt.Sprintf("%s/%d", network.String(), ones)
		}
	}
	if nic.Addr == "" || nic.MAC == "" || nic.LAN == "" {
		return NIC{}, fmt.Errorf("Wi-Fi address is incomplete")
	}
	for _, line := range strings.Split(arpText, "\n") {
		if !strings.Contains(line, gateway) {
			continue
		}
		fields := strings.Fields(line)
		for i, field := range fields {
			if field == "at" && i+1 < len(fields) {
				mac, ok := CanonicalMAC(fields[i+1])
				if ok {
					nic.GatewayMAC = mac
				}
			}
		}
	}
	if nic.GatewayMAC == "" {
		return NIC{}, fmt.Errorf("the Wi-Fi router has no hardware address")
	}
	return nic, nil
}

func maskOnes(mask string) (int, error) {
	raw := strings.TrimPrefix(strings.ToLower(mask), "0x")
	v, err := strconv.ParseUint(raw, 16, 32)
	if err != nil {
		return 0, err
	}
	ones := bits.OnesCount32(uint32(v))
	if ones < 8 || ones > 30 {
		return 0, fmt.Errorf("mask %s", mask)
	}
	return ones, nil
}
