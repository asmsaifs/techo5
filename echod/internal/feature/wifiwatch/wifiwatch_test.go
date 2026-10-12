package wifiwatch

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/HuskerMinion/techo5/echod/internal/lib/wifi"
)

// world is what the watcher sees, set by the test.
type world struct {
	now        time.Time
	ha, joined bool
	gateway    bool
	reassocs   int
	said       []string

	deaf     bool        // the radio hears nothing sent to a group
	noCounts bool        // the IP counters cannot be read
	quietNet bool        // a quiet network: the router's queries and the device's answers, nothing more
	advertAt []time.Time // when the router's adverts arrive, on a quiet network
	noIPv6   bool        // the IPv6 file is missed this look
	g        wifi.Group  // the group counters, IPv4 and IPv6 together
}

// groupPackets is the network between two looks: the device sends to groups itself and hears its own
// copies, and unless it is deaf it hears the rest of the network too. On a quiet network the rest is a
// membership query, answered with a report that does not loop back, and now and then a router advert.
func (wd *world) groupPackets() (wifi.Group, bool) {
	wd.g.Out += 5
	wd.g.In += 5
	if !wd.deaf {
		if wd.quietNet {
			wd.g.In++
			wd.g.Out++
			for len(wd.advertAt) > 0 && !wd.now.Before(wd.advertAt[0]) {
				wd.g.Router++
				wd.advertAt = wd.advertAt[1:]
			}
		} else {
			wd.g.In += 3
		}
	}
	g := wd.g
	g.From = wifi.FromIPv4 | wifi.FromIPv6
	if wd.noIPv6 {
		// What the IPv6 file adds is missing: about half of everything here.
		g.In, g.Out, g.Router, g.From = g.In/2, g.Out/2, 0, wifi.FromIPv4
	}
	return g, !wd.noCounts
}

func (wd *world) watch() *Watch {
	return &Watch{
		now:          func() time.Time { return wd.now },
		haConnected:  func() bool { return wd.ha },
		groupPackets: wd.groupPackets,
		joined:       func(context.Context) bool { return wd.joined },
		gatewayUp:    func(context.Context) bool { return wd.gateway },
		reassociate:  func(context.Context) error { wd.reassocs++; return nil },
		evidence:     func(context.Context) []string { return []string{"supplicant: wpa_state=COMPLETED"} },
		say: func(msg string, args ...any) {
			for i := 0; i+1 < len(args); i += 2 {
				if args[i] == "when" {
					msg += " " + args[i+1].(string)
				}
			}
			wd.said = append(wd.said, msg)
		},
		wait: firstWait,
	}
}

// count is how many times the watcher said something starting with prefix.
func (wd *world) count(prefix string) int {
	n := 0
	for _, s := range wd.said {
		if strings.HasPrefix(s, prefix) {
			n++
		}
	}
	return n
}

// What came of a reassociation is said once, either way, so a diagnostics download can tell a rekey
// that the watcher cured from one it did not.
func TestItSaysWhetherTheReassociationWorked(t *testing.T) {
	wd := start()
	w := wd.watch()
	wd.run(w, time.Minute)
	wd.ha = false
	wd.run(w, 4*time.Minute) // reassociates at 17:04
	if wd.count("wifi: evidence before") != 1 {
		t.Fatalf("no evidence recorded before reassociating: %q", wd.said)
	}
	wd.ha = true
	wd.run(w, 2*time.Minute)
	if wd.count("wifi: recovered") != 1 {
		t.Errorf("recovery said %d times, want once: %q", wd.count("wifi: recovered"), wd.said)
	}

	wd = start()
	w = wd.watch()
	wd.run(w, time.Minute)
	wd.ha = false
	wd.run(w, 10*time.Minute)
	if wd.count("wifi: still unreachable") != 1 || wd.count("wifi: evidence after") != 1 {
		t.Errorf("a reassociation that did not help was not said once with its evidence: %q", wd.said)
	}
	if wd.count("wifi: recovered") != 0 {
		t.Errorf("said it recovered when it did not: %q", wd.said)
	}
}

// run looks every 30 seconds for d.
func (wd *world) run(w *Watch, d time.Duration) {
	for end := wd.now.Add(d); wd.now.Before(end); wd.now = wd.now.Add(every) {
		w.look(context.Background())
	}
}

func start() *world {
	return &world{now: time.Date(2026, 9, 23, 17, 0, 0, 0, time.UTC), ha: true, joined: true, gateway: true}
}

func TestDeafToBroadcastsItReassociatesOnceAndBacksOff(t *testing.T) {
	wd := start()
	w := wd.watch()
	wd.run(w, time.Minute) // Home Assistant connected

	wd.ha = false // the rekey: joined, gateway answers, Home Assistant cannot reach us
	wd.run(w, 2*time.Minute)
	if wd.reassocs != 0 {
		t.Fatalf("reassociated %d times inside the grace a Home Assistant restart gets", wd.reassocs)
	}
	wd.run(w, 2*time.Minute)
	if wd.reassocs != 1 {
		t.Fatalf("reassociated %d times once Home Assistant had been gone long enough, want 1", wd.reassocs)
	}
	wd.run(w, 9*time.Minute)
	if wd.reassocs != 1 {
		t.Fatalf("reassociated again inside the first wait: %d", wd.reassocs)
	}
	// Home Assistant went at 17:01, so the first try was at 17:04, the second is due at 17:14 and the
	// third, the wait doubled, at 17:34.
	wd.run(w, 2*time.Minute) // to 17:16
	if wd.reassocs != 2 {
		t.Fatalf("no second try after the first wait: %d", wd.reassocs)
	}
	wd.run(w, 17*time.Minute) // to 17:33
	if wd.reassocs != 2 {
		t.Fatalf("the wait did not double: %d", wd.reassocs)
	}
	wd.run(w, 2*time.Minute) // past 17:34
	if wd.reassocs != 3 {
		t.Fatalf("no third try once the doubled wait was up: %d", wd.reassocs)
	}

	// Home Assistant back by itself, long after the try: the loss is over, but the wait starts over
	// only once it has stayed.
	wd.run(w, 3*time.Minute)
	wd.ha = true
	wd.run(w, time.Minute)
	if !w.lostAt.IsZero() || w.wait != mostWait {
		t.Errorf("back for a minute: wait %v, lost at %v; want the loss over and the wait kept", w.wait, w.lostAt)
	}
	wd.run(w, steady)
	if w.wait != firstWait {
		t.Errorf("an hour back did not start the wait over: %v", w.wait)
	}
}

// Home Assistant that comes and goes by itself (a server restarting over and over) is not taken for a
// run of cures: coming back right after a try looks the same either way, so the wait keeps growing
// until it has stayed.
func TestAFlappingHomeAssistantBacksOff(t *testing.T) {
	wd := start()
	w := wd.watch()
	wd.run(w, time.Minute)
	for range 12 { // an hour of 4 minutes gone, 1 back
		wd.ha = false
		wd.run(w, 4*time.Minute)
		wd.ha = true
		wd.run(w, time.Minute)
	}
	if wd.reassocs > 3 {
		t.Errorf("reassociated %d times in an hour of flapping, want the backoff (at most 3)", wd.reassocs)
	}
}

// Home Assistant that stays a quarter of an hour starts the wait over, so a router that rekeys more
// often than hourly is cured after the first wait each time, not the longest.
func TestHomeAssistantThatStaysStartsTheWaitOver(t *testing.T) {
	wd := start()
	w := wd.watch()
	wd.run(w, time.Minute)
	for range 3 {
		wd.ha = false
		wd.run(w, 4*time.Minute)
		wd.ha = true
		wd.run(w, 25*time.Minute)
	}
	if wd.reassocs != 3 || w.wait != firstWait {
		t.Errorf("%d tries, wait %v; want 3 and the first wait", wd.reassocs, w.wait)
	}
}

func TestItLeavesAloneWhatAReassociationDoesNotCure(t *testing.T) {
	for _, tc := range []struct {
		name                 string
		everHA, joined, gwUp bool
	}{
		{"a device Home Assistant never connected to", false, true, true},
		{"a supplicant that is not joined", true, false, true},
		{"a gateway that does not answer (the network is down)", true, true, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			wd := start()
			wd.ha, wd.joined, wd.gateway = tc.everHA, tc.joined, tc.gwUp
			w := wd.watch()
			wd.run(w, time.Minute)
			wd.ha = false
			wd.run(w, time.Hour)
			if wd.reassocs != 0 {
				t.Errorf("reassociated %d times", wd.reassocs)
			}
		})
	}
}

// A device with a direct brain never has Home Assistant connected, so it goes by group traffic: when
// that stops while the device stays joined and the gateway answers, it is the rekey, and it is cured
// the same way, with the same grace and the same backoff.
func TestWithoutHomeAssistantItGoesByTrafficSentToEveryone(t *testing.T) {
	wd := start()
	wd.ha = false
	w := wd.watch()
	wd.run(w, time.Minute) // 17:00 is the first reading; the network is heard at 17:00:30

	wd.deaf = true // from 17:01: only its own copies come back
	wd.run(w, 22*time.Minute)
	if wd.reassocs != 0 {
		t.Fatalf("reassociated %d times before the network had been quiet for quiet and then gone", wd.reassocs)
	}
	wd.run(w, time.Minute) // quiet from 17:20:30, gone at 17:23:30
	if wd.reassocs != 1 {
		t.Fatalf("reassociated %d times once the network had been quiet long enough, want 1", wd.reassocs)
	}
	if wd.count("wifi: traffic sent to everyone has been gone") != 1 {
		t.Errorf("the log does not say what it went by: %q", wd.said)
	}
	wd.run(w, 9*time.Minute) // to 17:33; the second try is due at 17:33:30
	if wd.reassocs != 1 {
		t.Fatalf("reassociated again inside the first wait: %d", wd.reassocs)
	}

	wd.deaf = false
	wd.run(w, time.Minute)
	if wd.count("wifi: recovered: traffic sent to everyone") != 0 {
		t.Errorf("said it recovered long after the settle time: %q", wd.said)
	}
	if !w.lostAt.IsZero() {
		t.Errorf("hearing the network again did not end the loss: lost at %v", w.lostAt)
	}
}

// Back within the settle time after a reassociation is said as a recovery.
func TestWithoutHomeAssistantItSaysTheReassociationWorked(t *testing.T) {
	wd := start()
	wd.ha = false
	w := wd.watch()
	wd.run(w, time.Minute)
	wd.deaf = true
	wd.run(w, 23*time.Minute) // reassociates at 17:23:30
	if wd.reassocs != 1 {
		t.Fatalf("reassociated %d times, want 1", wd.reassocs)
	}
	wd.deaf = false
	wd.run(w, time.Minute)
	if wd.count("wifi: recovered: traffic sent to everyone is back") != 1 {
		t.Errorf("recovery not said once: %q", wd.said)
	}
}

func TestWithoutHomeAssistantItLeavesAloneWhatItCannotTell(t *testing.T) {
	for _, tc := range []struct {
		name        string
		before, now func(*world)
	}{
		{"a network that is never heard this boot", func(wd *world) { wd.deaf = true }, func(*world) {}},
		{"counters that cannot be read", func(wd *world) { wd.noCounts = true }, func(*world) {}},
		{"counters that stop being read once the network was heard", func(*world) {}, func(wd *world) { wd.noCounts = true }},
		{"a gateway that does not answer (the network is down)", func(wd *world) { wd.gateway = false }, func(*world) {}},
		{"a quiet network with no router adverts", func(wd *world) { wd.quietNet = true }, func(*world) {}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			wd := start()
			wd.ha = false
			tc.before(wd)
			w := wd.watch()
			wd.run(w, time.Minute)
			tc.now(wd)
			wd.deaf = true
			wd.run(w, time.Hour)
			if wd.reassocs != 0 {
				t.Errorf("reassociated %d times", wd.reassocs)
			}
		})
	}
}

// The first reading is totals since boot, which say the network was heard once, not that it still is:
// a radio deaf from the first look is never taken for one that hears.
func TestTheFirstReadingIsNotTakenAsHeard(t *testing.T) {
	wd := start()
	wd.ha = false
	wd.g = wifi.Group{In: 90000, Out: 1000}
	wd.deaf = true
	w := wd.watch()
	wd.run(w, 2*time.Hour)
	if wd.reassocs != 0 || w.sign != "" {
		t.Errorf("a deaf radio's totals since boot were taken as hearing: %d reassociations, sign %q", wd.reassocs, w.sign)
	}
}

// A look where one of the two files was missed, and the look after, when it is back, are new starts:
// what the missed file adds back would otherwise read as the network heard.
func TestAMissedFileIsNotTakenAsHeard(t *testing.T) {
	deafFor := func(miss bool) int {
		wd := start()
		wd.ha = false
		w := wd.watch()
		wd.run(w, time.Minute)
		wd.deaf = true
		for range 12 {
			wd.run(w, 4*time.Minute)
			wd.noIPv6 = miss
			wd.run(w, 30*time.Second)
			wd.noIPv6 = false
		}
		return wd.reassocs
	}
	if missed, want := deafFor(true), deafFor(false); missed != want || want == 0 {
		t.Errorf("reassociated %d times with a file missed now and then, %d without", missed, want)
	}
}

// On a quiet network the router's membership queries and the device's answers cancel out, and only
// its adverts, every ten minutes here, show the network is there: that is enough, for hours.
func TestAQuietNetworkIsHeardByItsRouterAdverts(t *testing.T) {
	wd := start()
	wd.ha = false
	wd.quietNet = true
	for at := wd.now.Add(time.Minute); at.Before(wd.now.Add(4 * time.Hour)); at = at.Add(10 * time.Minute) {
		wd.advertAt = append(wd.advertAt, at)
	}
	w := wd.watch()
	wd.run(w, 4*time.Hour)
	if wd.reassocs != 0 || w.sign != signGroup {
		t.Errorf("a quiet network was reassociated %d times (sign %q)", wd.reassocs, w.sign)
	}
}

// A network heard only now and then, less often than quiet, is reassociated with the wait growing: a
// moment of being heard between losses does not start it over.
func TestANetworkHeardNowAndThenIsNotALoop(t *testing.T) {
	wd := start()
	wd.ha = false
	wd.quietNet = true
	for at := wd.now.Add(time.Minute); at.Before(wd.now.Add(4 * time.Hour)); at = at.Add(25 * time.Minute) {
		wd.advertAt = append(wd.advertAt, at)
	}
	w := wd.watch()
	wd.run(w, 4*time.Hour)
	// Waits of 10, 20 and 40 minutes, then an hour: at most six tries in four hours.
	if wd.reassocs == 0 || wd.reassocs > 6 || w.wait != mostWait {
		t.Errorf("reassociated %d times in four hours, wait %v; want 1 to 6 and the wait at its longest", wd.reassocs, w.wait)
	}
}

// Once Home Assistant has connected it is the sign, as before: group traffic still arriving does not
// hide that Home Assistant can no longer reach the device.
func TestHomeAssistantStaysTheSignOnceItHasConnected(t *testing.T) {
	wd := start()
	w := wd.watch()
	wd.run(w, time.Minute)
	wd.ha = false
	wd.run(w, 5*time.Minute)
	if wd.reassocs != 1 {
		t.Fatalf("reassociated %d times with Home Assistant gone and the network still heard, want 1", wd.reassocs)
	}
	if wd.count("wifi: Home Assistant has been gone") != 1 {
		t.Errorf("the log does not name Home Assistant: %q", wd.said)
	}
}
