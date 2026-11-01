package via

import (
	"net"
	"strings"

	"github.com/hbar/hbar/internal/store"
)

// Decision is what Ano does with one name from a device that was added.
type Decision struct {
	Enrolled bool   `json:"enrolled"`
	Forward  bool   `json:"forward"`
	Answer   bool   `json:"answer"`
	Stream   bool   `json:"stream"`
	Category string `json:"category"`
}

// Decide forwards streaming, and forwards every other name unless a Refuse or Answer here rule is on.
func Decide(st *store.Store, addr, host string) (Decision, error) {
	id, _, enrolled, err := st.ViaDevice(addr)
	if err != nil || !enrolled {
		return Decision{}, err
	}
	host, ok := Normalize(host)
	if !ok {
		return Decision{Enrolled: true, Forward: true}, nil
	}
	out := Decision{Enrolled: true, Forward: true, Category: "outbound"}
	if Streaming(host) {
		out.Stream = true
		out.Category = "stream"
		return out, nil
	}
	status, action, found, err := st.ViaRule(id, host)
	if err != nil {
		return Decision{}, err
	}
	if found && status == "approved" && action == "drop" {
		out.Forward = false
	}
	if found && status == "approved" && action == "allow_envoy" {
		out.Forward = false
		out.Answer = true
	}
	return out, nil
}

func clientIP(addr net.Addr) string {
	return CallerIP(addr.String())
}

// CallerIP is the IPv4 address of a connection.
func CallerIP(remote string) string {
	host, _, err := net.SplitHostPort(remote)
	if err != nil {
		host = remote
	}
	host = strings.TrimPrefix(host, "::ffff:")
	ip := net.ParseIP(host)
	if ip == nil {
		return host
	}
	if v4 := ip.To4(); v4 != nil {
		return v4.String()
	}
	return ip.String()
}

func blockedTarget(host string, allowPrivate bool) bool {
	if allowPrivate {
		return false
	}
	if ip := net.ParseIP(host); ip != nil {
		return unroutable(ip)
	}
	ips, err := net.LookupIP(host)
	if err != nil {
		return false
	}
	for _, ip := range ips {
		if unroutable(ip) {
			return true
		}
	}
	return false
}

func unroutable(ip net.IP) bool {
	return ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() || ip.IsMulticast() || ip.IsUnspecified()
}
