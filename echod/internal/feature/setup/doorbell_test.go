package setup

import (
	"net/url"
	"path/filepath"
	"testing"

	"github.com/HuskerMinion/techo5/echod/internal/config"
)

// The Doorbell form: a camera only from the recorder's list, a port from 1 to 65535 with Reolink's
// own 8000 kept as the default, and None taking the doorbell away whatever the port field holds.
func TestDoorbellForm(t *testing.T) {
	config.Use(filepath.Join(t.TempDir(), "state.json"))
	if err := config.Set().Home().Reolink(config.Reolink{Base: "https://192.0.2.5", User: "admin",
		Cameras: []config.Camera{{Entity: "reolink:0", Name: "Front Door"}, {Entity: "reolink:1", Name: "Yard"}}}); err != nil {
		t.Fatal(err)
	}
	post := func(camera, port string) string {
		return saveDoorbell(form(url.Values{"token": {"t"}, "what": {"doorbell"}, "camera": {camera}, "port": {port}}))
	}
	if p := post("reolink:0", "8000"); p != "" {
		t.Fatal(p)
	}
	if d := config.Get().Doorbell; d.Camera != "reolink:0" || d.Port != 0 || d.ONVIFPort() != 8000 {
		t.Errorf("saved %+v: Reolink's default port is kept as none", d)
	}
	if p := post("reolink:1", "8001"); p != "" || config.Get().Doorbell != (config.Doorbell{Camera: "reolink:1", Port: 8001}) {
		t.Errorf("saved %+v (%s)", config.Get().Doorbell, p)
	}
	for _, bad := range [][2]string{{"reolink:7", "8000"}, {"camera.front_door", ""}, {"reolink:0", "0"}, {"reolink:0", "65536"}, {"reolink:0", "80a"}} {
		if p := post(bad[0], bad[1]); p == "" {
			t.Errorf("camera %q port %q was taken", bad[0], bad[1])
		}
	}
	if p := post("", "junk"); p != "" || config.Get().Doorbell != (config.Doorbell{}) {
		t.Errorf("None with a junk port: %q, saved %+v", p, config.Get().Doorbell)
	}
}
