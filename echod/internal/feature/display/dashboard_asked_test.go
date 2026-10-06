//go:build !dot && !spot

package display

import (
	"testing"
	"time"
)

// A dashboard Home Assistant put up stays past the time one opened by a finger is forgotten, until
// Home Assistant takes it down.
func TestDashboardShownByHomeAssistantStays(t *testing.T) {
	d := &Display{}
	d.dashboardAsked(true)
	if !d.dash || !d.dashHeld {
		t.Fatalf("dashboard_show: dash %v held %v, want both", d.dash, d.dashHeld)
	}
	d.dashTouched = time.Now().Add(-20 * time.Minute)
	d.dashScene(&scene{phase: "idle"}, false)
	if !d.dash {
		t.Fatal("a dashboard put up by Home Assistant was forgotten after the return time")
	}

	d.dashShowing = true
	d.dashboardAsked(false)
	if d.dash {
		t.Fatal("dashboard_hide left the dashboard up")
	}
}

// One opened by a finger is still forgotten.
func TestDashboardOpenedByHandIsForgotten(t *testing.T) {
	d := &Display{}
	d.dash, d.dashTouched = true, time.Now().Add(-20*time.Minute)
	d.dashScene(&scene{phase: "idle"}, false)
	if d.dash {
		t.Fatal("a dashboard opened by hand stayed past the return time")
	}
}

// With no deck server set a swipe in from the right does not open a deck, so the drawer keeps that
// edge; and putting the dashboard away puts a deck away too.
func TestDeckNeedsAServer(t *testing.T) {
	d := &Display{}
	if d.openStreamDeck() {
		t.Fatal("openStreamDeck opened a deck with no server set")
	}
	if d.streamDeck {
		t.Fatal("openStreamDeck left the deck asked for")
	}
	d.streamDeck, d.dashShowing = true, true
	d.closeDashboard()
	if d.streamDeck || d.dash {
		t.Fatalf("closeDashboard left deck %v dash %v up", d.streamDeck, d.dash)
	}
}
