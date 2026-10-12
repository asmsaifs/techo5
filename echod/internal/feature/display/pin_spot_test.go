//go:build spot

package display

import (
	"image"
	"path/filepath"
	"testing"
	"time"

	"github.com/HuskerMinion/techo5/echod/internal/config"
	"github.com/HuskerMinion/techo5/echod/internal/feature/security"
	"github.com/HuskerMinion/techo5/echod/internal/hardware/touch"
)

// A finger on a locked row reaches the PIN pad, not the setting, on the Spot's own path for taps.
func TestTheSpotsTapsGoThroughTheLock(t *testing.T) {
	config.Use(filepath.Join(t.TempDir(), "state.json"))
	if err := security.Get().SetPIN("2468"); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		pinPress("cancel")
		_ = security.Get().SetPIN("")
	})
	d := &Display{r: newRoundRenderer(image.NewRGBA(image.Rect(0, 0, side, side))), poke: make(chan struct{}, 1), sheetOpen: true}
	d.r.zones = []zone{{r: image.Rect(200, 200, 280, 280), kind: zoneRow, id: "ssh", label: "SSH"}}
	d.sheetGesture(touch.Gesture{Kind: touch.Tap, X: 240, Y: 240})
	if p := pinNow(time.Now()); !p.open || p.what != "SSH" || config.Get().Security.SSH {
		t.Fatalf("a tap on SSH: pad up %v for %q, SSH %v; want the pad for SSH and no change", p.open, p.what, config.Get().Security.SSH)
	}
}
