package gate

import (
	"context"
	"net"
	"testing"
)

func TestAddressesForNamesAddsThePacketAddress(t *testing.T) {
	prev := lookupIPs
	lookupIPs = func(context.Context, string) ([]net.IP, error) {
		return []net.IP{net.ParseIP("9.9.9.9"), net.ParseIP("2001:db8::1")}, nil
	}
	t.Cleanup(func() { lookupIPs = prev })
	got := AddressesForNames([]string{"metrics.example"})
	if len(got) != 1 || got[0] != "9.9.9.9" {
		t.Fatal(got)
	}
}
