package lan

import (
	"net"
	"os/exec"
	"strings"
)

// ListenAddr is this computer's address on the home Wi-Fi, and the DNS server
// Ano forwards streaming lookups to.
func ListenAddr() (addr, upstream string) {
	upstream = "1.1.1.1"
	link, ok := wifiLink()
	if !ok {
		return "", upstream
	}
	if link.Router != "" && net.ParseIP(link.Router) != nil {
		upstream = link.Router
	}
	out, err := exec.Command("ipconfig", "getifaddr", link.Iface).Output()
	ip := strings.TrimSpace(string(out))
	if err != nil || net.ParseIP(ip) == nil {
		return "", upstream
	}
	return ip, upstream
}
