package sun

import (
	"testing"
	"time"
)

// Against published almanac times, to within three minutes.
func TestTimes(t *testing.T) {
	london, _ := time.LoadLocation("Europe/London")
	denver, _ := time.LoadLocation("America/Denver")
	for _, c := range []struct {
		name      string
		lat, lon  float64
		day       time.Time
		rise, set string
	}{
		{"London, midsummer", 51.5074, -0.1278, time.Date(2024, 6, 21, 0, 0, 0, 0, london), "04:43", "21:21"},
		{"Denver, midwinter", 39.7392, -104.9903, time.Date(2024, 12, 21, 0, 0, 0, 0, denver), "07:18", "16:39"},
	} {
		rise, set, ok := Times(c.lat, c.lon, c.day)
		if !ok {
			t.Fatalf("%s: no sunrise", c.name)
		}
		for _, p := range []struct {
			got  time.Time
			want string
		}{{rise, c.rise}, {set, c.set}} {
			w, _ := time.ParseInLocation("15:04", p.want, c.day.Location())
			want := time.Date(c.day.Year(), c.day.Month(), c.day.Day(), w.Hour(), w.Minute(), 0, 0, c.day.Location())
			if d := p.got.Sub(want); d > 3*time.Minute || d < -3*time.Minute {
				t.Errorf("%s: got %s, want %s", c.name, p.got.Format("15:04"), p.want)
			}
		}
	}
}

// Above the Arctic Circle at midsummer the sun does not set.
func TestMidnightSun(t *testing.T) {
	if _, _, ok := Times(78.2, 15.6, time.Date(2024, 6, 21, 0, 0, 0, 0, time.UTC)); ok {
		t.Error("Svalbard at midsummer has a sunset")
	}
}
