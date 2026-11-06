package gate

import (
	"context"
	"net"
	"time"
)

var lookupIPs = func(ctx context.Context, name string) ([]net.IP, error) {
	return net.DefaultResolver.LookupIP(ctx, "ip4", name)
}

// AddressesForNames resolves refused names to the IPv4 addresses a packet will use.
func AddressesForNames(names []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, name := range names {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		found, err := lookupIPs(ctx, name)
		cancel()
		if err != nil {
			continue
		}
		for _, ip := range found {
			v4 := ip.To4()
			if v4 == nil || seen[v4.String()] {
				continue
			}
			seen[v4.String()] = true
			out = append(out, v4.String())
		}
	}
	return out
}

func uniqueIPs(ips []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, ip := range ips {
		if ip == "" || seen[ip] {
			continue
		}
		seen[ip] = true
		out = append(out, ip)
	}
	return out
}
