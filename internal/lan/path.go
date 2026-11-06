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

// ScreenAddr is this machine's address on the home network.
func ScreenAddr() string {
	if ip := addrOn(defaultIface()); ip != "" {
		return ip
	}
	ifaces, err := net.Interfaces()
	if err != nil {
		return ""
	}
	for _, iface := range ifaces {
		if ip := privateOn(iface); ip != "" {
			return ip
		}
	}
	return ""
}

func defaultIface() string {
	var out []byte
	var err error
	if _, statErr := exec.LookPath("ip"); statErr == nil {
		out, err = exec.Command("ip", "route", "show", "default").Output()
	} else {
		out, err = exec.Command("route", "-n", "get", "default").Output()
	}
	if err != nil {
		return ""
	}
	return ifaceFromDefaultRoute(string(out))
}

func ifaceFromDefaultRoute(text string) string {
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "interface:") {
			return strings.TrimSpace(strings.TrimPrefix(line, "interface:"))
		}
	}
	fields := strings.Fields(text)
	for i, field := range fields {
		if field == "dev" && i+1 < len(fields) {
			return fields[i+1]
		}
	}
	return ""
}

func addrOn(name string) string {
	if name == "" {
		return ""
	}
	iface, err := net.InterfaceByName(name)
	if err != nil {
		return ""
	}
	return privateOn(*iface)
}

func privateOn(iface net.Interface) string {
	if iface.Flags&net.FlagUp == 0 || iface.Flags&net.FlagLoopback != 0 {
		return ""
	}
	addrs, err := iface.Addrs()
	if err != nil {
		return ""
	}
	for _, addr := range addrs {
		ipnet, ok := addr.(*net.IPNet)
		if !ok {
			continue
		}
		ip := ipnet.IP.To4()
		if ip == nil || !ip.IsPrivate() || !ip.IsGlobalUnicast() {
			continue
		}
		return ip.String()
	}
	return ""
}
