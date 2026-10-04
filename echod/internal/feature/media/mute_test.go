package media

import (
	"path/filepath"
	"testing"

	esphome "github.com/ygelfand/go-esphome-device"

	"github.com/HuskerMinion/techo5/echod/internal/config"
)

// Muted, a level that moves on its own leaves the speaker silent, and a press on the device brings it
// back: somebody reaching for the volume wants to hear it.
func TestAPressUnmutes(t *testing.T) {
	config.Use(filepath.Join(t.TempDir(), "state.json"))
	p := &Player{mp: &esphome.MediaPlayer{}, night: newNight()}
	p.step.Store(10)
	p.Mute(true)
	p.setQuietly(6)
	if !p.muted.Load() || p.Volume() != 6 {
		t.Fatalf("a quiet change: muted %v at %d", p.muted.Load(), p.Volume())
	}
	p.Adjust(1)
	if p.muted.Load() {
		t.Error("a press left the speaker muted")
	}
	if got := p.Volume(); got != 7 {
		t.Errorf("a press up from 6 went to %d", got)
	}
}
