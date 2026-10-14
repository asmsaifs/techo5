//go:build spot

package display

import (
	"bytes"
	"image"
	"path/filepath"
	"testing"
	"time"

	"github.com/HuskerMinion/techo5/echod/internal/config"
	"github.com/HuskerMinion/techo5/echod/internal/feature/home"
)

// At the foot, the styles that have a small version leave the middle of the face to the photo, and
// the others are drawn as they are in the middle.
func TestSpotClockAtTheFoot(t *testing.T) {
	config.Use(filepath.Join(t.TempDir(), "state.json"))
	previewFont(t)
	t.Cleanup(func() {
		_ = config.Set().Screen().ClockStyle("")
		_ = config.Set().Screen().ClockPosition("")
	})
	at := time.Date(2026, 9, 16, 14, 7, 38, 0, time.Local)
	draw := func() *image.RGBA {
		img := image.NewRGBA(image.Rect(0, 0, side, side))
		newRoundRenderer(img).draw(roundScene{now: at, phase: "idle", weather: home.Weather{Condition: "sunny", Temp: "72°"},
			style: styleFacts{rise: at.Add(-7 * time.Hour), set: at.Add(5 * time.Hour), sunOK: true}})
		return img
	}
	inked := func(img *image.RGBA, band image.Rectangle) bool {
		for y := band.Min.Y; y < band.Max.Y; y++ {
			for x := band.Min.X; x < band.Max.X; x++ {
				if c := img.RGBAAt(x, y); c != img.RGBAAt(0, 0) {
					return true
				}
			}
		}
		return false
	}
	middle := image.Rect(center-100, center-60, center+100, center+40)
	for _, st := range clockStyles {
		if err := config.Set().Screen().ClockStyle(st.value); err != nil {
			t.Fatal(err)
		}
		setClockPosition(nil, 0)
		whole := draw()
		setClockPosition(nil, 1)
		foot := draw()
		if footStyle(st.value) {
			// Glow's colors fill the face behind the clock wherever it is.
			if st.value != styleGlow && inked(foot, middle) {
				t.Errorf("%s at the foot still draws across the middle", st.label)
			}
			if !inked(foot, image.Rect(center-110, 300, center+110, 440)) {
				t.Errorf("%s at the foot draws nothing there", st.label)
			}
		} else if !bytes.Equal(whole.Pix, foot.Pix) {
			t.Errorf("%s has no small version, but changed at the foot", st.label)
		}
	}
}
