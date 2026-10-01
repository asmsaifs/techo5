//go:build !dot

package dashboard

import (
	"context"
	"log/slog"
	"net"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/libp2p/zeroconf/v2"
)

// DeckService is what the desktop deck app advertises on the network (TXT: name, v).
const DeckService = "_techo5deck._tcp"

// deckLook is how long one look for decks listens: the answers come in well under a second on a home
// network, and the setup page waits for it.
const deckLook = 2 * time.Second

// FoundDeck is a deck app heard on the network.
type FoundDeck struct {
	Name    string // what the app calls itself, usually the computer's name
	Address string // host:port, ready for SetDeck
}

// Discover looks for deck apps for a moment and lists them by name. Finding one only saves typing
// its address: the key is still entered by hand, since nothing heard on the network is trusted.
func Discover(ctx context.Context) []FoundDeck {
	found := make(chan *zeroconf.ServiceEntry, 16)
	var entries []*zeroconf.ServiceEntry
	done := make(chan struct{})
	go func() {
		defer close(done)
		for e := range found {
			entries = append(entries, e)
		}
	}()
	look, cancel := context.WithTimeout(ctx, deckLook)
	defer cancel()
	// Browse closes the channel itself once it has started, and leaves it alone when it fails before;
	// the failure path ends the reader by hand, as announce.browse does.
	if err := zeroconf.Browse(look, DeckService, "local.", found); err != nil {
		slog.Info("deck: looking for decks failed", "err", err)
		func() {
			defer func() { _ = recover() }()
			close(found)
		}()
		<-done
		return nil
	}
	<-done
	return decks(entries)
}

// decks turns what was heard into a list: one entry per name, IPv4 preferred, sorted.
func decks(entries []*zeroconf.ServiceEntry) []FoundDeck {
	seen := map[string]bool{}
	var out []FoundDeck
	for _, e := range entries {
		addr := ""
		for _, ip := range e.AddrIPv4 {
			if ip.IsGlobalUnicast() {
				addr = ip.String()
				break
			}
		}
		if addr == "" {
			for _, ip := range e.AddrIPv6 {
				if ip.IsGlobalUnicast() {
					addr = "[" + ip.String() + "]"
					break
				}
			}
		}
		if addr == "" || e.Port == 0 {
			continue
		}
		name := e.Instance
		for _, t := range e.Text {
			if v, ok := strings.CutPrefix(t, "name="); ok && v != "" {
				name = v
			}
		}
		// A deck answers once per interface; the same name twice is one deck.
		if seen[strings.ToLower(name)] {
			continue
		}
		seen[strings.ToLower(name)] = true
		out = append(out, FoundDeck{Name: name, Address: net.JoinHostPort(strings.Trim(addr, "[]"), strconv.Itoa(e.Port))})
	}
	sort.Slice(out, func(i, j int) bool { return strings.ToLower(out[i].Name) < strings.ToLower(out[j].Name) })
	return out
}
