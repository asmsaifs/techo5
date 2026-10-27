//go:build !dot

package display

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/HuskerMinion/techo5/echod/internal/config"
)

// The weekend's nights are set apart from the others, one end at a time or by the action, and go back
// to the same as every other night (#305).
func TestWeekendNightHours(t *testing.T) {
	config.Use(filepath.Join(t.TempDir(), "state.json"))
	d := &Display{poke: make(chan struct{}, 1)}
	d.weekendStart, d.weekendEnd = weekendEndSelect(d, true), weekendEndSelect(d, false)
	if err := config.Set().Screen().Night("22-7"); err != nil {
		t.Fatal(err)
	}

	// One end chosen starts from the other nights' hours.
	d.weekendEnd.OnCommand("09:30")
	if got := config.Get().Screen.NightWeekend; got != "22:00-09:30" {
		t.Fatalf("weekend end 09:30: %q", got)
	}
	d.weekendStart.OnCommand("23:30")
	if got := config.Get().Screen.NightWeekend; got != "23:30-09:30" {
		t.Fatalf("weekend start 23:30: %q", got)
	}
	// Friday 23:00 is a weekend night's evening, not yet begun; a Thursday's at 23:00 has.
	fri := time.Date(2026, 10, 23, 23, 0, 0, 0, time.Local)
	if nightNow(fri) || !nightNow(fri.AddDate(0, 0, -1)) {
		t.Error("Friday evening went by the other nights' hours")
	}
	if !nightNow(time.Date(2026, 10, 25, 9, 0, 0, 0, time.Local)) {
		t.Error("Sunday 9:00 is not night with the weekend's ending at 9:30")
	}

	if err := d.setWeekendNightHours("8", "8"); err == nil {
		t.Error("a weekend night that starts as it ends was taken")
	}
	if err := d.setWeekendNightHours("0:00", "9:00"); err != nil || config.Get().Screen.NightWeekend != "0-9" {
		t.Errorf("the action: %v, %q", err, config.Get().Screen.NightWeekend)
	}
	d.weekendStart.OnCommand(sameAsNights)
	if got := config.Get().Screen.NightWeekend; got != "" {
		t.Errorf("same as other nights: %q", got)
	}
}
