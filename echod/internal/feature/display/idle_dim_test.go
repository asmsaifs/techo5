//go:build !dot && !spot

package display

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/HuskerMinion/techo5/echod/internal/config"
)

// Dim after idle takes the screen down to its Dimmest once it has gone the wait untouched, not before,
// not at night or while something is going on, and a touch brings it straight back.
func TestIdleDimsAndATouchUndims(t *testing.T) {
	config.Use(filepath.Join(t.TempDir(), "config.json"))
	if err := config.Set().Screen().DimAfterMinutes(5); err != nil {
		t.Fatal(err)
	}
	d := &Display{on: true, ceiling: 100, poke: make(chan struct{}, 1)}
	start := time.Now()
	d.relight(true)
	full := d.level

	d.idleDimTick(start, true, false, false) // the first look starts the wait
	d.idleDimTick(start.Add(4*time.Minute), true, false, false)
	if d.idleDimmed {
		t.Fatal("dimmed before the wait")
	}
	d.idleDimTick(start.Add(4*time.Minute), true, true, false) // a turn: the wait starts again
	d.idleDimTick(start.Add(8*time.Minute), true, false, false)
	if d.idleDimmed {
		t.Fatal("dimmed four minutes after a turn")
	}
	d.idleDimTick(start.Add(9*time.Minute+time.Second), true, false, true)
	if d.idleDimmed {
		t.Fatal("dimmed at night, which has its own rules")
	}
	d.idleDimTick(start.Add(9*time.Minute+time.Second), true, false, false)
	if !d.idleDimmed {
		t.Fatal("not dimmed after the wait")
	}
	for range 100 {
		d.settleStep()
	}
	if want := idleDimLevel(full, 100); d.level > want+0.5 || d.level >= full {
		t.Fatalf("dimmed to %.1f, want %.1f (full %.1f)", d.level, want, full)
	}

	d.mu.Lock()
	d.touchedAt = start.Add(10 * time.Minute)
	d.mu.Unlock()
	d.idleDimTick(start.Add(10*time.Minute), true, false, false)
	if d.idleDimmed || d.level != full {
		t.Fatalf("a touch left it dimmed (%v) at %.1f, want %.1f at once", d.idleDimmed, d.level, full)
	}

	if err := config.Set().Screen().DimAfterMinutes(0); err != nil {
		t.Fatal(err)
	}
	d.idleDimTick(start.Add(time.Hour), true, false, false)
	if d.idleDimmed {
		t.Fatal("dimmed with Dim after idle at Never")
	}
}
