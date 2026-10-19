package diag

import (
	"net"
	"testing"
)

// The IPv4 addresses come first, so a VPN's address is not pushed past what Home Assistant keeps.
func TestIPv4AddressesComeFirst(t *testing.T) {
	got := addressList([]net.IP{
		net.ParseIP("192.168.1.20"), net.ParseIP("2001:db8::1"), net.ParseIP("2001:db8::2"), net.ParseIP("100.64.0.10"),
	})
	if want := "192.168.1.20, 100.64.0.10, 2001:db8::1, 2001:db8::2"; got != want {
		t.Errorf("%q, want %q", got, want)
	}
}
