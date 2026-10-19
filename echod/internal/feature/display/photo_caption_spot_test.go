//go:build spot

package display

import (
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/HuskerMinion/techo5/echod/internal/config"
	"github.com/HuskerMinion/techo5/echod/internal/feature/home"
)

// The day and place of the photo are written low on the round face; with none, the face is as it was.
// With SPOT_PREVIEW set, each is written there.
func TestPhotoCaptionOnTheRoundScreensaver(t *testing.T) {
	at := time.Date(2026, 10, 17, 9, 41, 0, 0, time.Local)
	photo := image.NewRGBA(image.Rect(0, 0, side, side))
	for y := 0; y < side; y++ {
		for x := 0; x < side; x++ {
			photo.SetRGBA(x, y, color.RGBA{uint8(60 + 140*y/side), uint8(70 + 60*x/side), 90, 255})
		}
	}
	caps := []home.PhotoCaption{{When: time.Date(2019, 6, 15, 14, 30, 0, 0, time.UTC), Place: "Springfield, Ohio"}}
	for _, overlay := range []string{config.SlideshowOverlayOff, config.SlideshowOverlaySmall, ""} {
		draw := func(c []home.PhotoCaption) *image.RGBA {
			img := image.NewRGBA(image.Rect(0, 0, side, side))
			newRoundRenderer(img).draw(roundScene{now: at, phase: "idle", slideshowScreensaver: photo, slideshowOverlay: overlay, slideshowCaptions: c})
			return img
		}
		plain, captioned := draw(nil), draw(caps)
		differ := false
		for i := range plain.Pix {
			differ = differ || plain.Pix[i] != captioned.Pix[i]
		}
		if !differ {
			t.Errorf("overlay %q: the caption changed nothing", overlay)
		}
		if dir := os.Getenv("SPOT_PREVIEW"); dir != "" {
			name := "caption-spot-" + map[string]string{config.SlideshowOverlayOff: "noclock", config.SlideshowOverlaySmall: "small", "": "clock"}[overlay] + ".png"
			f, err := os.Create(filepath.Join(dir, name))
			if err != nil {
				t.Fatal(err)
			}
			if err := png.Encode(f, captioned); err != nil {
				t.Fatal(err)
			}
			f.Close()
		}
	}
}
