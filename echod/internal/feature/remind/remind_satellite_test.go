package remind

import (
	"net/http"
	"net/http/httptest"

	"github.com/HuskerMinion/techo5/echod/internal/lib/hass"
	"strings"
	"testing"
)

// The satellite is asked for by the device's name, in quotes the name does not contain, since Home
// Assistant's templates take no escapes; a name no quotes can hold is not asked for.
func TestSatelliteTemplate(t *testing.T) {
	for name, want := range map[string]string{
		"Kitchen":    `'name', 'Kitchen')`,
		"Kids' Room": `'name', "Kids' Room")`,
	} {
		got, ok := satelliteTemplate(name)
		if !ok || !strings.Contains(got, want) || !strings.HasPrefix(got, "{{ states.assist_satellite") {
			t.Errorf("%q: template %s", name, got)
		}
	}
	for _, name := range []string{`Den's "Echo"`, `A\B`} {
		if _, ok := satelliteTemplate(name); ok {
			t.Errorf("%q was asked for", name)
		}
	}
}

// Entities from a Home Assistant that refuses the template API (401) still find the satellite, in any
// language, and never another device's (#294).
func TestSatelliteFromStatesAlone(t *testing.T) {
	states := `[
	 {"entity_id":"light.echo","attributes":{"friendly_name":"Echo Light"}},
	 {"entity_id":"assist_satellite.kitchen_echo_assist_satellit","attributes":{"friendly_name":"Kitchen Echo Assist-Satellit"}},
	 {"entity_id":"assist_satellite.echo_assist_satellit","attributes":{"friendly_name":"Echo Assist-Satellit"}}]`
	list, err := parse(states)
	if err != nil {
		t.Fatal(err)
	}
	if id, ok := matchSatellite(list, "Echo", "assist_satellite.echo_assist_satellite"); !ok || id != "assist_satellite.echo_assist_satellit" {
		t.Errorf("got %s %v", id, ok)
	}
}

func parse(states string) ([]hass.Entity, error) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/template" {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		_, _ = w.Write([]byte(states))
	}))
	defer srv.Close()
	c := hass.NewTestClient(srv.URL, "tok", srv.Client())
	return c.Entities("assist_satellite")
}

func TestMatchSatellite(t *testing.T) {
	l := []hass.Entity{
		{ID: "assist_satellite.kitchen_echo_x", Name: "Kitchen Echo Assist-Satellit"},
		{ID: "assist_satellite.kitchen_assist_satellite", Name: "Kitchen Assist-Satellit"},
	}
	if id, ok := matchSatellite(l, "kitchen", "assist_satellite.kitchen_assist_satellite"); !ok || id != "assist_satellite.kitchen_assist_satellite" {
		t.Errorf("two prefixed: %s %v", id, ok)
	}
	if _, ok := matchSatellite(l, "Kitchen", "assist_satellite.kitchen_other"); ok {
		t.Error("ambiguous names picked one")
	}
	if _, ok := matchSatellite(l, "Den", "x"); ok {
		t.Error("another device's satellite taken")
	}
	one := []hass.Entity{{ID: "assist_satellite.e", Name: "Echo Assist satellite"}}
	if id, ok := matchSatellite(one, "Echo", ""); !ok || id != "assist_satellite.e" {
		t.Error("english name")
	}
}
