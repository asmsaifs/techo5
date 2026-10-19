package assistant

import "testing"

// A tool that is not the device's own is held to the same lockout: once a round has brought outside words
// in, from the next round on only what looks things up runs.
func TestTheGateForOtherTools(t *testing.T) {
	g := NewTurnGate()
	if ok, _ := g.Allow(false); !ok {
		t.Fatal("a change refused before anything was read")
	}
	g.Saw(true)
	if ok, _ := g.Allow(false); !ok {
		t.Fatal("a change in the same round as the reading was refused: it was asked for before the reading was seen")
	}
	g.EndRound()
	if ok, why := g.Allow(false); ok || why == "" {
		t.Fatal("a change allowed after reading outside words")
	}
	if ok, _ := g.Allow(true); !ok {
		t.Fatal("a look-up refused after reading")
	}
	if !g.Read() {
		t.Fatal("the turn does not say it read outside words")
	}
}

func TestTheGateWithNothingRead(t *testing.T) {
	g := NewTurnGate()
	g.Saw(false)
	g.EndRound()
	if ok, _ := g.Allow(false); !ok || g.Read() {
		t.Fatal("refused with nothing outside read")
	}
	if len(g.Tools()) == 0 {
		t.Fatal("no tools")
	}
}
