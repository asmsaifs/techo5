//go:build !dot

package dashboard

import (
	"net"
	"testing"

	"github.com/libp2p/zeroconf/v2"
)

func TestDecks(t *testing.T) {
	entry := func(inst string, port int, txt []string, v4 ...string) *zeroconf.ServiceEntry {
		e := &zeroconf.ServiceEntry{Port: port, Text: txt}
		e.Instance = inst
		for _, a := range v4 {
			e.AddrIPv4 = append(e.AddrIPv4, net.ParseIP(a))
		}
		return e
	}
	got := decks([]*zeroconf.ServiceEntry{
		entry("studio-mac", 9555, []string{"name=Studio Mac", "v=1"}, "192.168.1.20"),
		entry("studio-mac", 9555, []string{"name=Studio Mac"}, "192.168.1.20"), // second interface
		entry("anon", 9556, nil, "192.168.1.21"),                               // name from the instance
		entry("noaddr", 9555, nil),                                             // nothing to dial
		entry("loop", 9555, nil, "127.0.0.1"),                                  // not routable
		entry("noport", 0, nil, "192.168.1.22"),
	})
	want := []FoundDeck{{"anon", "192.168.1.21:9556"}, {"Studio Mac", "192.168.1.20:9555"}}
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("deck %d: got %v, want %v", i, got[i], want[i])
		}
	}
}
