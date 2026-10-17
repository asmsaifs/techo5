//go:build !dot

package display

import (
	"fmt"
	"path/filepath"
	"testing"

	"github.com/HuskerMinion/techo5/echod/internal/config"
)

// The alarm editor's Light only row (#164) turns the draft light only, and saving keeps it; a silent
// alarm, Home Assistant's, has no such row.
func TestTheAlarmEditorSetsLightOnly(t *testing.T) {
	config.Use(filepath.Join(t.TempDir(), "state.json"))
	d := &Display{poke: make(chan struct{}, 1)}
	d.EditNewAlarm()
	card := alarmEditorCard(sheetView{}, *d.draft)
	found := false
	for _, r := range card.rows {
		found = found || r.id == "e.lightonly"
	}
	if !found {
		t.Fatal("no Light only row in the editor")
	}
	if !d.alarmRowTap("e.lightonly", partMain, -1) || !d.draft.alarm.LightOnly {
		t.Fatal("the row did not turn the draft light only")
	}
	d.actionTap("save")
	list := config.Get().Alarms.List
	if len(list) != 1 || !list[0].LightOnly {
		t.Fatalf("saved %+v", list)
	}

	silent := alarmEditorCard(sheetView{}, alarmDraft{alarm: config.Alarm{Silent: true}})
	for _, r := range silent.rows {
		if r.id == "e.lightonly" {
			t.Error("a silent alarm offered Light only")
		}
	}
}

// A light-only alarm always has a light, so the editor's Wake with light row says the minutes it gets
// where its own setting would say Off, or Default with no default.
func TestTheLightOnlyRowSaysItsLight(t *testing.T) {
	config.Use(filepath.Join(t.TempDir(), "state.json"))
	for _, tc := range []struct {
		al   config.Alarm
		want string
	}{
		{config.Alarm{Sunrise: config.SunriseOff}, "Off"},
		{config.Alarm{}, "Default"},
		{config.Alarm{LightOnly: true, Sunrise: config.SunriseOff}, fmt.Sprintf("%d min", config.LightOnlyMinutes)},
		{config.Alarm{LightOnly: true}, fmt.Sprintf("%d min", config.LightOnlyMinutes)},
		{config.Alarm{LightOnly: true, Sunrise: 10}, "10 min"},
	} {
		if got := alarmSunriseValue(tc.al); got != tc.want {
			t.Errorf("%+v: the row says %q, want %q", tc.al, got, tc.want)
		}
	}
	if err := config.Set().Alarms().SunriseMinutes(15); err != nil {
		t.Fatal(err)
	}
	if got := alarmSunriseValue(config.Alarm{LightOnly: true}); got != "Default" {
		t.Errorf("with a default the row says %q, want Default", got)
	}
	if got := alarmSunriseValue(config.Alarm{LightOnly: true, Sunrise: config.SunriseOff}); got != "15 min" {
		t.Errorf("with the light off and a default of 15 the row says %q", got)
	}
}
