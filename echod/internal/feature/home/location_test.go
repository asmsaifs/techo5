package home

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/ygelfand/go-esphome-device/api"

	"github.com/HuskerMinion/techo5/echod/internal/config"
	"github.com/HuskerMinion/techo5/echod/internal/feature/hastate"
)

// A device given a zone of its own centers the rain map and alerts there; one without is at home; and
// one whose zone Home Assistant has not described says so, rather than showing home's.
func TestLocationFollowsItsZone(t *testing.T) {
	config.Use(filepath.Join(t.TempDir(), "state.json"))
	for _, v := range []struct{ entity, attr, value string }{
		{"zone.home", "latitude", "39.7"}, {"zone.home", "longitude", "-104.9"},
		{"zone.cabin", "latitude", "40.1"}, {"zone.cabin", "longitude", "-104.8"},
	} {
		if err := hastate.Get().Handle(context.Background(), nil,
			&api.HomeAssistantStateResponse{EntityId: v.entity, Attribute: v.attr, State: v.value}); err != nil {
			t.Fatal(err)
		}
	}

	at := func(want float64) {
		t.Helper()
		lat, _, err := homeLocation()
		if err != nil || lat != want {
			t.Errorf("with location %q: latitude %v, %v; want %v", config.Get().Home.Location, lat, err, want)
		}
	}
	at(39.7)

	if err := config.Set().Home().Location("zone.cabin"); err != nil {
		t.Fatal(err)
	}
	at(40.1)

	if err := config.Set().Home().Location("zone.nowhere"); err != nil {
		t.Fatal(err)
	}
	if _, _, err := homeLocation(); err == nil {
		t.Error("a zone Home Assistant has not described gave a location")
	}
}
