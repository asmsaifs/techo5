//go:build spot

package display

import (
	"fmt"
	"image"
	"image/png"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/HuskerMinion/techo5/echod/internal/config"
	"github.com/HuskerMinion/techo5/echod/internal/feature/home"
)

// Every weather at every part of the day draws behind the round clock; with SPOT_PREVIEW set, each is
// written there to look at.
func TestSpotWeatherArtDraws(t *testing.T) {
	config.Use(filepath.Join(t.TempDir(), "state.json"))
	dir := os.Getenv("SPOT_PREVIEW")
	at := time.Date(2026, 9, 16, 14, 7, 38, 0, time.Local)
	for c, cond := range map[artCond]string{artClear: "sunny", artPartly: "partlycloudy", artRain: "rainy", artStorm: "lightning-rainy", artSnow: "snowy", artFog: "fog"} {
		for when := artDawn; when <= artNight; when++ {
			base, clouds := paintLandscape(artKey{c, when, side, side, 42})
			frame := image.NewRGBA(base.Rect)
			composeArt(frame, base, clouds, at.Unix())
			img := image.NewRGBA(image.Rect(0, 0, side, side))
			newRoundRenderer(img).draw(roundScene{now: at, phase: "idle", weather: home.Weather{Condition: cond, Temp: "72°"}, slideshow: frame, artFx: fxFor(cond)})
			if dir == "" {
				continue
			}
			f, err := os.Create(filepath.Join(dir, fmt.Sprintf("art-%s-%d.png", cond, when)))
			if err != nil {
				t.Fatal(err)
			}
			png.Encode(f, img)
			f.Close()
		}
	}
}
