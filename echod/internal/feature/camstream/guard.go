package camstream

import (
	"log/slog"
	"net/netip"
	"sync"
	"time"
)

// A wrong login, over RTSP or ONVIF, is answered only after wrongDelay, and an address that gets lockAfter
// of them wrong within lockWindow is refused for lockFor: the password is all that stands between the
// network and the room, and this keeps guessing at it slow. Logins being checked count as wrong ones
// until they are known not to be, so asking many at once is no faster than asking one after another.
var wrongDelay = time.Second

const (
	lockAfter  = 10
	lockWindow = 10 * time.Minute
	lockFor    = 15 * time.Minute

	// allAfter is how many wrong logins from anywhere, within lockWindow, stop every new login for lockFor:
	// guessing from many addresses at once is no way round the count of one. An address that logged in
	// within knownFor (the house's own recorder) is not stopped by it, nor counted in it: a stranger
	// guessing cannot keep the recorder out. It still has the count of its own address.
	allAfter = 50
	knownFor = 24 * time.Hour

	// guardKept is how many addresses are remembered. Only those with nothing against them are let go to
	// make room; with none to let go, a login from an address not yet known is refused.
	guardKept = 256
)

// guard counts wrong logins by address, and all of them together.
type guard struct {
	mu       sync.Mutex
	by       map[string]*record
	all      []time.Time          // every wrong login
	allUntil time.Time            // every new login refused until then
	known    map[string]time.Time // the addresses that logged in, and when they last did
}

type record struct {
	fails    []time.Time
	until    time.Time
	checking int // logins being checked now
}

// keyOf is the address a login is counted against: an IPv6 address on the internet by its /64, which one
// household holds whole, so rotating through it is no way round the count; anything else as it is.
func keyOf(from string) string {
	a, err := netip.ParseAddr(from)
	if err != nil {
		return from
	}
	a = a.Unmap()
	if a.Is6() && !a.IsLinkLocalUnicast() && !a.IsPrivate() && !a.IsLoopback() {
		if p, err := a.Prefix(64); err == nil {
			return p.String()
		}
	}
	return a.String()
}

// refused is whether an address is locked out now, or every login is.
func (g *guard) refused(from string) bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	now := time.Now()
	if now.Before(g.allUntil) && !g.isKnown(keyOf(from), now) {
		return true
	}
	r := g.by[keyOf(from)]
	return r != nil && now.Before(r.until)
}

// attempt is a login from an address about to be checked: false when the address is locked out, or has
// as many wrong and unsettled logins as it may. Otherwise done is called with whether it was wrong, and
// waits wrongDelay when it was.
func (g *guard) attempt(from string) (ok bool, done func(wrong bool)) {
	from = keyOf(from)
	g.mu.Lock()
	now := time.Now()
	r := g.record(from, now)
	if r == nil || (now.Before(g.allUntil) && !g.isKnown(from, now)) || now.Before(r.until) ||
		countRecent(r.fails, now)+r.checking >= lockAfter {
		g.mu.Unlock()
		return false, nil
	}
	r.checking++
	g.mu.Unlock()
	return true, func(wrong bool) {
		if wrong {
			g.failed(from)
			time.Sleep(wrongDelay)
		}
		g.mu.Lock()
		r.checking--
		if !wrong {
			g.loggedIn(from, time.Now())
		}
		g.mu.Unlock()
	}
}

// record is an address's, made when it has none; nil when there is no room for it. g.mu is held.
func (g *guard) record(from string, now time.Time) *record {
	if g.by == nil {
		g.by = map[string]*record{}
	}
	r := g.by[from]
	if r != nil {
		return r
	}
	if len(g.by) >= guardKept {
		for k, v := range g.by {
			if v.checking == 0 && !now.Before(v.until) && countRecent(v.fails, now) == 0 {
				delete(g.by, k)
			}
		}
	}
	if len(g.by) >= guardKept {
		// Still full, all of it wrong logins not yet run out: none of them is forgotten to make room,
		// which would wipe its count. The new address waits until one runs out.
		return nil
	}
	r = &record{}
	g.by[from] = r
	return r
}

// failed counts a wrong login, locking the address out at lockAfter, and every login at allAfter.
func (g *guard) failed(from string) {
	from = keyOf(from)
	g.mu.Lock()
	defer g.mu.Unlock()
	now := time.Now()
	if !g.isKnown(from, now) {
		g.all = append(recent(g.all, now), now)
	}
	if len(g.all) >= allAfter && !now.Before(g.allUntil) {
		g.allUntil = now.Add(lockFor)
		g.all = nil
		slog.Warn("camera streaming: too many wrong logins from everywhere; every login refused for a while", "for", lockFor)
	}
	r := g.record(from, now)
	if r == nil {
		return
	}
	r.fails = append(recent(r.fails, now), now)
	if len(r.fails) >= lockAfter && !now.Before(r.until) {
		r.until = now.Add(lockFor)
		r.fails = nil
		slog.Warn("camera streaming: an address locked out after wrong logins", "from", from, "for", lockFor)
	}
}

// isKnown is whether an address logged in within knownFor. g.mu is held.
func (g *guard) isKnown(from string, now time.Time) bool {
	at, ok := g.known[from]
	return ok && now.Sub(at) < knownFor
}

// loggedIn notes an address that gave the right login, forgetting those that have not for knownFor, and
// the longest ago when there are guardKept. g.mu is held.
func (g *guard) loggedIn(from string, now time.Time) {
	if g.known == nil {
		g.known = map[string]time.Time{}
	}
	oldest := ""
	for k, at := range g.known {
		if now.Sub(at) >= knownFor {
			delete(g.known, k)
		} else if oldest == "" || at.Before(g.known[oldest]) {
			oldest = k
		}
	}
	if _, ok := g.known[from]; !ok && len(g.known) >= guardKept {
		delete(g.known, oldest)
	}
	g.known[from] = now
}

// countRecent is how many failures are within lockWindow of now, leaving the list as it is.
func countRecent(ts []time.Time, now time.Time) int {
	n := 0
	for _, t := range ts {
		if now.Sub(t) < lockWindow {
			n++
		}
	}
	return n
}

// recent is the failures within lockWindow of now, kept in the list's own storage: assign it back.
func recent(ts []time.Time, now time.Time) []time.Time {
	kept := ts[:0]
	for _, t := range ts {
		if now.Sub(t) < lockWindow {
			kept = append(kept, t)
		}
	}
	return kept
}
