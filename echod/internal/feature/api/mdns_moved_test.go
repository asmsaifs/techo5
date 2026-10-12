package api

import (
	"net"
	"testing"
)

// Only an IPv4 address giving way to another is a move, which drops Home Assistant's connections and
// keeps the device unannounced for two minutes: IPv6 addresses arriving with SLAAC or rotating, and a
// first lease at boot, cost nothing.
func TestOnlyANewIPv4AddressIsAMove(t *testing.T) {
	ip := func(s ...string) []net.IP {
		var out []net.IP
		for _, a := range s {
			out = append(out, net.ParseIP(a))
		}
		return out
	}
	for _, c := range []struct {
		name     string
		was, now []net.IP
		want     bool
	}{
		{"a fixed address replaces the lease", ip("192.0.2.61"), ip("192.0.2.62"), true},
		{"the same address", ip("192.0.2.61"), ip("192.0.2.61"), false},
		{"a first lease", nil, ip("192.0.2.61"), false},
		{"the address lost", ip("192.0.2.61"), nil, false},
		{"SLAAC after the lease", ip("192.0.2.61"), ip("192.0.2.61", "2001:db8::5"), false},
		{"a temporary address rotating", ip("192.0.2.61", "2001:db8::5"), ip("192.0.2.61", "2001:db8::9"), false},
		{"moved with IPv6 beside it", ip("192.0.2.61", "2001:db8::5"), ip("192.0.2.62", "2001:db8::5"), true},
		{"a second address beside it (a VPN coming up)", ip("192.0.2.61"), ip("100.64.0.10", "192.0.2.61"), false},
		{"the second address going", ip("100.64.0.10", "192.0.2.61"), ip("192.0.2.61"), false},
		{"the LAN address gone, the VPN's kept", ip("100.64.0.10", "192.0.2.61"), ip("100.64.0.10", "192.0.2.62"), true},
	} {
		if got := movedIPv4(c.was, c.now); got != c.want {
			t.Errorf("%s: moved %v, want %v", c.name, got, c.want)
		}
	}
}

// The advertising loop's view of the addresses over time: what it holds, and when it calls a move.
// Each step is the addresses seen, whether a setting is on trial then, and whether that is a move. The
// loop is given the Wi-Fi's addresses alone (metrics.WiFiAddresses); here, the VPN's (vpn, vpn2) are
// left out the same way.
func TestTheLoopHoldsWhatHomeAssistantKnows(t *testing.T) {
	ip := func(s ...string) []net.IP {
		var out []net.IP
		for _, a := range s {
			out = append(out, net.ParseIP(a))
		}
		return out
	}
	type seen struct {
		ips      []net.IP
		trialing bool
		want     moveStep
	}
	const lan1, lan2, vpn, vpn2 = "192.0.2.61", "192.0.2.62", "100.64.0.10", "198.51.100.11"
	// The VPN's addresses are on another interface; the rest are the Wi-Fi's.
	tunnel := func(a net.IP) bool { return a.Equal(net.ParseIP(vpn)) || a.Equal(net.ParseIP(vpn2)) }
	onWiFi := func(ips []net.IP) []net.IP {
		var out []net.IP
		for _, a := range ips {
			if !tunnel(a) {
				out = append(out, a)
			}
		}
		return out
	}
	for _, c := range []struct {
		name  string
		steps []seen
	}{
		{"the LAN address lost and another found beside a VPN's", []seen{
			{ip(lan1, vpn), false, stepAdvertise},
			{ip(vpn), false, stepAdvertise}, // the fixed address flushed for DHCP
			{ip(lan2, vpn), false, stepMoved},
			{ip(lan2, vpn), false, stepAdvertise},
		}},
		{"the VPN back with a new address is no move", []seen{
			{ip(lan1, vpn), false, stepAdvertise},
			{ip(lan1), false, stepAdvertise}, // the VPN down
			{ip(lan1, vpn2), false, stepAdvertise},
			{ip(lan1, vpn), false, stepAdvertise},
		}},
		{"the VPN's address changing is no move", []seen{
			{ip(lan1, vpn), false, stepAdvertise},
			{ip(lan1, vpn2), false, stepAdvertise},
		}},
		{"the LAN address replaced through no address at all", []seen{
			{ip(lan1), false, stepAdvertise},
			{ip(), false, stepAdvertise},
			{ip(lan2), false, stepMoved},
		}},
		{"the LAN address replaced while the VPN also moves", []seen{
			{ip(lan1, vpn), false, stepAdvertise},
			{ip(vpn2), false, stepAdvertise},
			{ip(lan2, vpn2), false, stepMoved},
		}},
		{"a trial beside a VPN that fails is no move", []seen{
			{ip(lan1, vpn), false, stepAdvertise},
			{ip(lan2, vpn), true, stepWait},
			{ip(lan1, vpn), true, stepAdvertise},
			{ip(lan1, vpn), false, stepAdvertise},
		}},
		{"a VPN coming up and going is no move", []seen{
			{ip(lan1), false, stepAdvertise},
			{ip(lan1, vpn), false, stepAdvertise},
			{ip(lan1), false, stepAdvertise},
			{ip(lan1, vpn), false, stepAdvertise},
		}},
		{"fixed to DHCP through no address at all", []seen{
			{ip(lan1), false, stepAdvertise},
			{ip("2001:db8::5"), false, stepAdvertise},
			{ip(lan2), false, stepMoved},
		}},
		{"a trial that fails is no move", []seen{
			{ip(lan1), false, stepAdvertise},
			{ip(lan2), true, stepWait},
			{ip(lan2), true, stepWait},
			{ip(lan1), true, stepAdvertise},
			{ip(lan1), false, stepAdvertise},
		}},
		{"a trial that works is a move once it is over", []seen{
			{ip(lan1), false, stepAdvertise},
			{ip(lan2), true, stepWait},
			{ip(lan2), false, stepMoved},
			{ip(lan2), false, stepAdvertise},
		}},
	} {
		var held []net.IP
		for i, s := range c.steps {
			var got moveStep
			held, got = nextHeld(held, onWiFi(s.ips), s.trialing)
			if got != s.want {
				t.Errorf("%s, step %d (%s): %d, want %d", c.name, i, v4Key(s.ips), got, s.want)
			}
		}
	}
}
