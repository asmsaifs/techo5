//go:build mockup && !dot && !spot

package display

import (
	"image"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/HuskerMinion/techo5/echod/internal/config"
	"github.com/HuskerMinion/techo5/echod/internal/feature/home"
	"github.com/HuskerMinion/techo5/echod/internal/feature/timer"
	"github.com/HuskerMinion/techo5/echod/internal/lib/hass"
)

// Mockups of every clock style at each Clock position, over a stand-in photo (MOCK_OUT).
func TestMockClockCorners(t *testing.T) {
	config.Use(filepath.Join(t.TempDir(), "state.json"))
	previewFont(t)
	out := os.Getenv("MOCK_OUT")
	at := time.Date(2026, 9, 16, 14, 7, 38, 0, time.Local)
	var week []hass.Day
	for i, c := range []string{"partlycloudy", "rainy", "sunny", "cloudy", "snowy"} {
		week = append(week, hass.Day{When: at.AddDate(0, 0, i), Condition: c, High: float64(72 - 3*i), Low: float64(51 - 2*i)})
	}
	facts := styleFacts{
		rise: time.Date(2026, 9, 16, 6, 48, 0, 0, time.Local), set: time.Date(2026, 9, 16, 19, 5, 0, 0, time.Local), sunOK: true,
		days: week,
		next: []hass.Event{
			{Summary: "Dentist", Start: at.Add(83 * time.Minute), End: at.Add(143 * time.Minute)},
			{Summary: "Soccer practice", Start: at.Add(233 * time.Minute), End: at.Add(293 * time.Minute)},
		},
		places: worldPlaces(nil),
	}
	for _, panel := range []struct {
		name       string
		wide, high int
	}{{"show5", showWide, showHigh}, {"show8", show8Wide, show8High}} {
		var tiles []mockTile
		for _, st := range clockStyles {
			if err := config.Set().Screen().ClockStyle(st.value); err != nil {
				t.Fatal(err)
			}
			for _, pos := range clockPositions {
				if err := config.Set().Screen().ClockPosition(pos.value); err != nil {
					t.Fatal(err)
				}
				img := image.NewRGBA(image.Rect(0, 0, panel.wide, panel.high))
				s := scene{now: at, phase: "idle", weather: home.Weather{Condition: "partlycloudy", Temp: "72°"}, style: facts,
					slideshow: mockPhoto(panel.wide, panel.high)}
				newRenderer(img).draw(s)
				name := panel.name + "-" + st.label + "-" + pos.label
				mockSave(t, out, name, img)
				tiles = append(tiles, mockTile{st.label + " / " + pos.label, img})
				// The same with a timer running, for comparing (not on the sheet).
				img = image.NewRGBA(image.Rect(0, 0, panel.wide, panel.high))
				s.timers = []timer.Countdown{{Name: "pasta", Left: 272 * time.Second, Total: 600 * time.Second, Active: true}}
				newRenderer(img).draw(s)
				mockSave(t, out, name+"-timer", img)
			}
		}
		mockSheet(t, out, panel.name+"-sheet", tiles, 3)
	}
}
