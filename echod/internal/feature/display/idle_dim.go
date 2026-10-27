//go:build !dot

package display

import (
	"time"

	"github.com/HuskerMinion/techo5/echod/internal/config"
)

// Dim after idle (#275): the Show's screen goes down to its Dimmest once nobody has touched it, spoken to
// it or, with presence detection on, been seen near it for a while, and comes back up at the next of
// them. Not at night, which has its own rules (night.go), and not while anything is going on.

// idleDims are the choices, in the order the screen and Home Assistant offer them; 0 minutes is never.
var idleDims = []struct {
	label   string
	minutes int
}{
	{"Never", 0},
	{"1 minute", 1},
	{"2 minutes", 2},
	{"5 minutes", 5},
	{"10 minutes", 10},
	{"15 minutes", 15},
	{"30 minutes", 30},
}

// idleDimOptions are the choices' labels.
func idleDimOptions() []string {
	out := make([]string, len(idleDims))
	for i, t := range idleDims {
		out[i] = t.label
	}
	return out
}

// idleDimIndex is the saved choice's place in idleDims; a value no choice has reads as Never.
func idleDimIndex() int {
	v := config.Get().Screen.DimAfterMinutes
	for i, t := range idleDims {
		if t.minutes == v {
			return i
		}
	}
	return 0
}

// idleDimAfter is how long the screen waits before dimming, 0 for never.
func idleDimAfter() time.Duration {
	return time.Duration(idleDims[idleDimIndex()].minutes) * time.Minute
}
