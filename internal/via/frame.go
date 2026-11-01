package via

import (
	"encoding/binary"
	"fmt"
	"net"
	"strings"
)

// SteerPort is where Ano accepts TCP from Fire TVs brought onto the path.
const SteerPort = 10082

// ARPReply builds one Ethernet ARP reply. dst and src are link addresses.
// sender is the address being claimed.
func ARPReply(dstMAC, srcMAC, senderMAC, senderIP, targetMAC, targetIP string) ([]byte, error) {
	dst, err := macBytes(dstMAC)
	if err != nil {
		return nil, err
	}
	src, err := macBytes(srcMAC)
	if err != nil {
		return nil, err
	}
	smac, err := macBytes(senderMAC)
	if err != nil {
		return nil, err
	}
	tmac, err := macBytes(targetMAC)
	if err != nil {
		return nil, err
	}
	sip := net.ParseIP(senderIP).To4()
	tip := net.ParseIP(targetIP).To4()
	if sip == nil || tip == nil {
		return nil, fmt.Errorf("address must be IPv4")
	}
	frame := make([]byte, 42)
	copy(frame[0:6], dst)
	copy(frame[6:12], src)
	binary.BigEndian.PutUint16(frame[12:14], 0x0806)
	binary.BigEndian.PutUint16(frame[14:16], 1)
	binary.BigEndian.PutUint16(frame[16:18], 0x0800)
	frame[18] = 6
	frame[19] = 4
	binary.BigEndian.PutUint16(frame[20:22], 2)
	copy(frame[22:28], smac)
	copy(frame[28:32], sip)
	copy(frame[32:38], tmac)
	copy(frame[38:42], tip)
	return frame, nil
}

func macBytes(mac string) ([]byte, error) {
	parts := strings.Split(mac, ":")
	if len(parts) != 6 {
		return nil, fmt.Errorf("mac %s", mac)
	}
	out := make([]byte, 6)
	for i, part := range parts {
		if len(part) != 2 {
			return nil, fmt.Errorf("mac %s", mac)
		}
		var v byte
		for _, c := range part {
			v <<= 4
			switch {
			case c >= '0' && c <= '9':
				v |= byte(c - '0')
			case c >= 'a' && c <= 'f':
				v |= byte(c-'a') + 10
			case c >= 'A' && c <= 'F':
				v |= byte(c-'A') + 10
			default:
				return nil, fmt.Errorf("mac %s", mac)
			}
		}
		out[i] = v
	}
	return out, nil
}

// RenderPF is the packet filter for Fire TVs Ano is bringing through.
// Traffic inside the LAN is left alone. TCP 80 and 443 toward the internet
// come to Ano. Other internet traffic is forwarded to the real router.
func RenderPF(iface, gateway, lanCIDR, listenIP string, addrs []string) (string, error) {
	if !safeIface(iface) || net.ParseIP(gateway) == nil || net.ParseIP(listenIP) == nil {
		return "", fmt.Errorf("missing Wi-Fi path")
	}
	if _, _, err := net.ParseCIDR(lanCIDR); err != nil {
		return "", err
	}
	var ips []string
	for _, addr := range addrs {
		if net.ParseIP(addr) == nil || strings.Contains(addr, "/") {
			return "", fmt.Errorf("address %s", addr)
		}
		ips = append(ips, addr)
	}
	if len(ips) == 0 {
		return "table <ano_tv> persist\n", nil
	}
	list := strings.Join(ips, ", ")
	return fmt.Sprintf(`table <ano_tv> persist { %s }
nat on %s inet from <ano_tv> to ! %s -> (%s)
rdr pass on %s inet proto tcp from <ano_tv> to ! %s port { 80, 443 } -> %s port %d
pass in quick on %s route-to (%s %s) inet from <ano_tv> to %s keep state
pass in quick on %s route-to (%s %s) inet from <ano_tv> to ! %s keep state
`, list, iface, lanCIDR, iface, iface, lanCIDR, listenIP, SteerPort, iface, iface, gateway, gateway, iface, iface, gateway, lanCIDR), nil
}

func safeIface(iface string) bool {
	if iface == "" || len(iface) > 8 {
		return false
	}
	for _, r := range iface {
		if (r < 'a' || r > 'z') && (r < '0' || r > '9') {
			return false
		}
	}
	return true
}
