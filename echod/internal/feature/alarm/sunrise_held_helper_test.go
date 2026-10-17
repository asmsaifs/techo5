package alarm

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/ygelfand/go-esphome-device/api"

	"github.com/HuskerMinion/techo5/echod/internal/config"
	"github.com/HuskerMinion/techo5/echod/internal/feature/hastate"
)

// The light held for a helper followed from Home Assistant comes back after a restart, though the
// helper is no alarm until Home Assistant says what it is set to; it used to be dropped every time.
// One whose time has gone by is let go, and the saved copy with it.
func TestTheHeldLightForAHelperComesBackAfterARestart(t *testing.T) {
	config.Use(filepath.Join(t.TempDir(), "state.json"))
	const helper = "input_datetime.held_light_restart"
	if err := config.Set().Alarms().Follow([]string{helper}); err != nil {
		t.Fatal(err)
	}
	if err := config.Set().Alarms().SunriseMinutes(30); err != nil {
		t.Fatal(err)
	}
	at := time.Now().Add(20 * time.Minute).Truncate(time.Minute)
	if err := config.Set().Alarms().Held(&config.Held{Key: helper, At: at}); err != nil {
		t.Fatal(err)
	}

	a := build()
	a.Restore(config.Get())
	if a.bridge.key != helper {
		t.Fatalf("before Home Assistant has said, the held light was let go (held for %q)", a.bridge.key)
	}
	if config.Get().Alarms.Held == nil {
		t.Error("before Home Assistant has said, the saved held light is gone")
	}

	// Home Assistant says what the helper is set to, and the light is held at full again.
	state := &api.HomeAssistantStateResponse{EntityId: helper, State: at.Format("15:04:05")}
	if err := hastate.Get().Handle(context.Background(), nil, state); err != nil {
		t.Fatal(err)
	}
	if got := a.SunriseProgress(time.Now()); got != 1 {
		t.Errorf("once Home Assistant has said, the held light is %.2f, want full", got)
	}

	// Saved for a moment that went by while the device was down, it does not come back.
	if err := config.Set().Alarms().Held(&config.Held{Key: helper, At: time.Now().Add(-5 * time.Minute)}); err != nil {
		t.Fatal(err)
	}
	late := build()
	late.Restore(config.Get())
	if late.bridge.key != "" {
		t.Errorf("a held light whose alarm went by is held for %q", late.bridge.key)
	}
	if h := config.Get().Alarms.Held; h != nil {
		t.Errorf("a held light whose alarm went by is still saved: %+v", h)
	}
}
