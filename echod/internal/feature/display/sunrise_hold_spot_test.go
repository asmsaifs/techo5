//go:build spot

package display

import (
	"image"
	"image/png"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// The Spot's Alarms card with the wake light held after an alarm, for looking at with SPOT_PREVIEW set.
func TestHeldLightRowDrawsRound(t *testing.T) {
	s := spotScene(catAlarms)
	s.sheet.st.cardScroll = 1000
	for name, hold := range map[string]struct {
		until  time.Time
		snooze bool
	}{
		"settings-alarms-light-held":    {s.now.Add(18 * time.Minute), false},
		"settings-alarms-light-snoozed": {s.now.Add(6 * time.Minute), true},
	} {
		heldLight(t, hold.until, hold.snooze, true)
		img := image.NewRGBA(image.Rect(0, 0, side, side))
		newRoundRenderer(img).draw(s)
		dir := os.Getenv("SPOT_PREVIEW")
		if dir == "" {
			continue
		}
		f, err := os.Create(filepath.Join(dir, name+".png"))
		if err != nil {
			t.Fatal(err)
		}
		if err := png.Encode(f, img); err != nil {
			t.Fatal(err)
		}
		if err := f.Close(); err != nil {
			t.Fatal(err)
		}
	}
}
