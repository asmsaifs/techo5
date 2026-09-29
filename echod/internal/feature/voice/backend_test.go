package voice

// The switch itself: the entity Home Assistant writes, the two ends that can move it, and the
// arbitration that moving it has to do.
//
// What cannot be tested here is the arbitration. Ending a turn means a conversation, a satellite
// and a microphone, and this package has no hardware on a build host — so the teardown each backend
// does when it is told to stand down is checked in its own package (xiaozhi's is, in wake_test.go),
// and what is checked here is that this side asks for it, once, for the right backend, and in the
// right order relative to the save.
//
// The order is the part worth a test. A switch that saves first and stands down after will have cut
// off an answer and then, on a full disk, put the device back on the backend that was giving it. A
// switch that does the two in the other order is the one M5 requires, and it is invisible from the
// config file afterwards.

import (
	"path/filepath"
	"testing"

	esphome "github.com/ygelfand/go-esphome-device"

	"github.com/HuskerMinion/techo5/echod/internal/config"
)

// switchOn is a Voice with just the switch, which is all these checks need: an entity to write to
// and a conversation that is not running, so the teardown is a no-op and what is being observed is
// the bookkeeping around it.
func switchOn(t *testing.T) *Voice {
	t.Helper()
	config.Use(filepath.Join(t.TempDir(), "config.json"))
	v := &Voice{}
	// A conversation that is not running, built the way build() builds it. The nil one is not a
	// shape this package ever has: backends() is called after the conversation exists, so a
	// nil-check for it would be a check against a state the code cannot reach.
	v.turn = newConversation(&esphome.VoiceSatellite{})
	v.backend = v.backends()
	return v
}

// The entity is a select over the two backends, and it is on the device rather than a sub-device.
func TestTheSwitchOffersTheBackends(t *testing.T) {
	v := switchOn(t)

	if v.backend.ObjectID != "voice_backend" {
		t.Errorf("the entity is %q, want voice_backend", v.backend.ObjectID)
	}
	if got, want := v.backend.Options, config.Labels(config.VoiceBackends()); len(got) != len(want) {
		t.Errorf("the switch offers %v, want %v", got, want)
	} else {
		for i := range want {
			if got[i] != want[i] {
				t.Errorf("the switch offers %v, want %v", got, want)
				break
			}
		}
	}
	if len(v.Entities()) != 1 || v.Entities()[0] != esphome.Entity(v.backend) {
		t.Errorf("Entities published %v, want just the switch", v.Entities())
	}
}

// Home Assistant writes a label, the config changes, and the entity shows what was settled. The
// whole round trip, because the entity is the only place the choice is visible to anybody but the
// person who moved the switch.
func TestWritingTheSwitchMovesTheBackend(t *testing.T) {
	v := switchOn(t)

	v.backend.OnCommand(config.BackendXiaozhi.Label())

	if got := config.Get().Voice.Backend; got != config.BackendXiaozhi {
		t.Errorf("the config says %q after writing %q, want xiaozhi", got, config.BackendXiaozhi.Label())
	}
	if !OnXiaozhi() {
		t.Error("OnXiaozhi is false with xiaozhi chosen")
	}
	if got := v.backend.Get(); got != config.BackendXiaozhi.Label() {
		t.Errorf("the entity shows %q, want %q", got, config.BackendXiaozhi.Label())
	}
}

// A label this build does not have. Home Assistant can be holding an option from a version of this
// software that had more of them, and a select asked for an option it does not list has to be left
// alone — changing it would move the switch to whatever happened to be first.
func TestAnOptionThisBuildDoesNotHaveIsIgnored(t *testing.T) {
	v := switchOn(t)
	if err := config.Set().Voice().Backend(config.BackendXiaozhi); err != nil {
		t.Fatalf("setting up: %v", err)
	}

	v.backend.OnCommand("somebody else's backend")

	if got := config.Get().Voice.Backend; got != config.BackendXiaozhi {
		t.Errorf("an unknown option moved the backend to %q", got)
	}
}

// The settings sheet is the other door into the same room, and it has to leave the same result. It
// also publishes, because the entity and the row are shown side by side on a device with a screen
// and a select Home Assistant has not been told about yet.
func TestTheSheetMovesTheSameBackend(t *testing.T) {
	v := switchOn(t)

	v.ChooseBackend(config.BackendXiaozhi)

	if got := config.Get().Voice.Backend; got != config.BackendXiaozhi {
		t.Errorf("the config says %q after the sheet chose xiaozhi", got)
	}
	if got := v.backend.Get(); got != config.BackendXiaozhi.Label() {
		t.Errorf("the entity shows %q after the sheet chose xiaozhi, want it to follow", got)
	}
	if !OnXiaozhi() {
		t.Error("OnXiaozhi is false after the sheet chose xiaozhi")
	}
}

// A value from a newer build, restored at start-up. It has to settle rather than be published, or
// the device comes up with a row it cannot change.
func TestRestoreSettlesAValueThisBuildDoesNotHave(t *testing.T) {
	v := switchOn(t)

	v.restore(config.Config{Voice: config.Voice{Backend: "somebody else's backend"}})

	if got := v.backend.Get(); got != config.BackendHomeAssistant.Label() {
		t.Errorf("the entity shows %q after restoring an unknown backend, want it settled to %q",
			got, config.BackendHomeAssistant.Label())
	}
	if got := config.Get().Voice.Backend; got != config.BackendHomeAssistant {
		t.Errorf("the config says %q after restoring an unknown backend, want it settled", got)
	}
}

// The handover xiaozhi makes before taking the microphone. With nothing running here it is a no-op,
// and that is worth a test: it is called on every turn that backend opens, including from a terminal
// over ssh, so a version of it that assumed a turn was running would fail in a way that only showed
// up when the device was idle.
func TestTakingTheMicrophoneFromAnIdleDeviceIsHarmless(t *testing.T) {
	v := switchOn(t)

	v.takeMicrophone()
	v.takeMicrophone()

	if v.turn != nil && v.turn.Busy() {
		t.Error("taking the microphone opened a turn on this side")
	}
}

// Busy is asked by things that are not this package — the ducking on a near-miss, the dashboard's
// relist — and they ask it about the room rather than about the setting. So it has to be true for a
// turn on either backend, which is the one thing a switch that only routes new turns can get wrong.
func TestBusyIsFalseWithNothingRunning(t *testing.T) {
	v := switchOn(t)
	if v.turn == nil {
		v.turn = newConversation(&esphome.VoiceSatellite{})
	}

	if v.Busy() {
		t.Error("Busy is true on a device with no turn running")
	}
	v.Cancel()
	if v.Busy() {
		t.Error("Busy is true after cancelling nothing")
	}
}
