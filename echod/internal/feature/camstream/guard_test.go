package camstream

import (
	"strconv"
	"testing"
	"time"
)

func noDelay(t *testing.T) {
	was := wrongDelay
	wrongDelay = 0
	t.Cleanup(func() { wrongDelay = was })
}

func wrong(g *guard, from string) {
	if ok, done := g.attempt(from); ok {
		done(true)
	}
}

// An IPv6 address on the internet is counted by its /64: rotating through one is no way round the
// count. A private one is counted as itself.
func TestIPv6IsCountedByItsNetwork(t *testing.T) {
	noDelay(t)
	var g guard
	for i := range lockAfter {
		wrong(&g, "2001:db8:1:2::"+strconv.Itoa(i+1))
	}
	if !g.refused("2001:db8:1:2::ffff") {
		t.Error("another address in the same /64 is not locked out")
	}
	if g.refused("2001:db8:1:3::1") {
		t.Error("another /64 is locked out")
	}
	if keyOf("fd00::1") != "fd00::1" || keyOf("fe80::1") != "fe80::1" {
		t.Error("a private or link-local address is counted by its network")
	}
}

// Wrong logins from many addresses add up: at allAfter, every new login is refused for a while.
func TestWrongLoginsFromEverywhereAddUp(t *testing.T) {
	noDelay(t)
	var g guard
	for i := range allAfter {
		wrong(&g, "192.0.2."+strconv.Itoa(i+1))
	}
	if !g.refused("198.51.100.1") {
		t.Error("a new address can still try after wrong logins from everywhere")
	}
}

// A full guard keeps every address with wrong logins against it, and refuses a new one rather than
// forget a count.
func TestAFullGuardForgetsNoCount(t *testing.T) {
	noDelay(t)
	var g guard
	g.by = map[string]*record{}
	now := time.Now()
	for i := range guardKept {
		g.by["k"+strconv.Itoa(i)] = &record{fails: []time.Time{now}}
	}
	if ok, _ := g.attempt("198.51.100.200"); ok {
		t.Error("a new address was let in by forgetting a count")
	}
	if len(g.by) != guardKept {
		t.Errorf("%d addresses remembered, want %d", len(g.by), guardKept)
	}
}

// Wrong logins from everywhere do not keep out an address that logged in lately: the house's own
// recorder goes on recording while a stranger guesses. A new address is still kept out.
func TestARecorderThatLoggedInIsNotKeptOut(t *testing.T) {
	noDelay(t)
	var g guard
	ok, done := g.attempt("192.168.1.20")
	if !ok {
		t.Fatal("the recorder's first login was refused")
	}
	done(false)
	for i := range allAfter {
		ok, done := g.attempt("198.51.100." + strconv.Itoa(i))
		if !ok {
			t.Fatalf("guess %d refused", i)
		}
		done(true)
	}
	if !g.refused("203.0.113.5") {
		t.Error("a new address is let in after wrong logins from everywhere")
	}
	if g.refused("192.168.1.20") {
		t.Error("the recorder that logged in is kept out")
	}
	if ok, done := g.attempt("192.168.1.20"); !ok {
		t.Error("the recorder cannot log in")
	} else {
		done(false)
	}
}
