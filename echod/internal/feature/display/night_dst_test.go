//go:build !dot && !spot

package display

import (
	"testing"
	"time"
	_ "time/tzdata" // the zone below, whatever the machine running the test has
)

// The hours are by the clock on the wall, so on the night the clocks fall back a night that ends at 6
// ends at 6, not at 5.
func TestNightChangeOnADaylightSavingDay(t *testing.T) {
	denver, err := time.LoadLocation("America/Denver")
	if err != nil {
		t.Fatal(err)
	}
	at := time.Date(2026, 10, 31, 23, 0, 0, 0, denver) // clocks go back at 2:00 on November 1
	got := nextNightChange("22-6", at)
	if want := time.Date(2026, 11, 1, 6, 0, 0, 0, denver); !got.Equal(want) {
		t.Errorf("the night ends at %v, want %v", got, want)
	}
}
