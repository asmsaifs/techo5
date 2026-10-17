//go:build !dot

package display

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/HuskerMinion/techo5/echod/internal/config"
)

// Night mode turned on from Home Assistant starts the night now: a tap a moment ago (on this screen's
// own dashboard, say, which is what turned the night on) does not hold it back. Turned off, the touch
// is kept.
func TestNightModeTurnedOnIsNow(t *testing.T) {
	config.Use(filepath.Join(t.TempDir(), "state.json"))
	now := time.Now()
	d := &Display{touchedAt: now, viewAt: now}
	sw := nightModeSwitch(d)

	sw.OnCommand(false)
	if d.touchedAt.IsZero() {
		t.Fatal("turning night mode off forgot the last touch")
	}
	sw.OnCommand(true)
	if !d.touchedAt.IsZero() || !d.viewAt.IsZero() {
		t.Errorf("turning night mode on kept the grace: touched %v, view %v", d.touchedAt, d.viewAt)
	}
}
