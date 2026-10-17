package voice

import (
	"path/filepath"
	"sync/atomic"
	"testing"

	esphome "github.com/ygelfand/go-esphome-device"

	"github.com/HuskerMinion/techo5/echod/internal/config"
	"github.com/HuskerMinion/techo5/echod/internal/feature/ring"
)

// ringing stands in for an alarm sounding: ring.IsSounding says so and ring.End stops it.
var ringing atomic.Bool

func init() { ring.Silences(func() bool { return ringing.Swap(false) }) }

// stopRoom is a device with nothing going on, whose music and held wake light the test sets: playing
// is the music, and the count is how often the held light was asked to let go.
func stopRoom(t *testing.T, playing bool) (*Voice, *int, *int) {
	t.Helper()
	config.Use(filepath.Join(t.TempDir(), "state.json"))
	wasPlaying, wasPause, wasRelease := musicPlaying, pauseMusic, releaseRing
	t.Cleanup(func() { musicPlaying, pauseMusic, releaseRing = wasPlaying, wasPause, wasRelease })
	var released, paused int
	musicPlaying = func() bool { return playing }
	pauseMusic = func() { playing = false; paused++ }
	releaseRing = func() bool { released++; return true }
	return &Voice{turn: newConversation(&esphome.VoiceSatellite{})}, &released, &paused
}

// sound starts a ring for the rest of the test.
func sound(t *testing.T) {
	t.Helper()
	ringing.Store(true)
	done := ring.Sounding()
	t.Cleanup(func() { ringing.Store(false); done() })
}

// The stop word and the action button with nothing sounding let go of the wake light held after an
// alarm (#161); with an alarm ringing they stop that, and with music playing they pause it, and the
// light stays either way.
func TestStopWithNothingSoundingLetsGoOfTheHeldLight(t *testing.T) {
	for _, c := range []struct {
		name    string
		ring    bool
		music   bool
		stop    func(v *Voice)
		release int
		pause   int
	}{
		{"stop word, nothing sounding", false, false, func(v *Voice) { v.Interrupt() }, 1, 0},
		{"Stop, nothing sounding", false, false, func(v *Voice) { v.Stop() }, 1, 0},
		{"stop word over an alarm", true, false, func(v *Voice) { v.Interrupt() }, 0, 0},
		{"Stop over an alarm", true, false, func(v *Voice) { v.Stop() }, 0, 0},
		{"stop word over music", false, true, func(v *Voice) { v.Interrupt() }, 0, 1},
		{"Stop over music", false, true, func(v *Voice) { v.Stop() }, 0, 1},
	} {
		t.Run(c.name, func(t *testing.T) {
			v, released, paused := stopRoom(t, c.music)
			if c.ring {
				sound(t)
			}
			c.stop(v)
			if *released != c.release || *paused != c.pause {
				t.Errorf("let go of the light %d times and paused %d, want %d and %d", *released, *paused, c.release, c.pause)
			}
			if c.ring && ringing.Load() {
				t.Error("the alarm was not stopped")
			}
		})
	}
}
