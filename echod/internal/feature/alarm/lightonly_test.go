package alarm

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/HuskerMinion/techo5/echod/internal/config"
	"github.com/HuskerMinion/techo5/echod/internal/feature/ring"
)

// A light-only alarm (#164) makes no sound and leaves nothing to stop: its time is the event, and the
// night ended, as the Night mode switch turned off ends it.
func TestALightOnlyAlarmEndsTheNight(t *testing.T) {
	config.Use(filepath.Join(t.TempDir(), "state.json"))
	got := events(t)
	a := build()
	endRing(t, a)
	now := time.Now()
	a.fire(source{key: "wake", label: "Morning", lightOnly: true, sunrise: 20}, now)
	if a.Ringing() || ring.IsSounding() {
		t.Error("a light-only alarm rang")
	}
	e := waitFor(t, got, 1)
	if len(e) != 1 || e[0]["event"] != "light_only" || e[0]["label"] != "Morning" {
		t.Errorf("events %v", e)
	}
	sc := config.Get().Screen
	if sc.NightOverride != "off" || sc.NightOverrideAt != now.Unix() {
		t.Errorf("night mode %q at %d, want off at %d", sc.NightOverride, sc.NightOverrideAt, now.Unix())
	}
}

// A light-only alarm always has its light: the alarm's own, the device's default, or LightOnlyMinutes,
// even with the alarm's light set to off. A silent one still has none.
func TestALightOnlyAlarmAlwaysHasALight(t *testing.T) {
	var none config.Alarms
	withDefault := config.Alarms{SunriseMinutes: 15}
	for _, tc := range []struct {
		al   config.Alarm
		in   config.Alarms
		want int
	}{
		{config.Alarm{LightOnly: true}, none, config.LightOnlyMinutes},
		{config.Alarm{LightOnly: true, Sunrise: config.SunriseOff}, none, config.LightOnlyMinutes},
		{config.Alarm{LightOnly: true}, withDefault, 15},
		{config.Alarm{LightOnly: true, Sunrise: 45}, withDefault, 45},
		{config.Alarm{Sunrise: config.SunriseOff}, withDefault, 0},
		{config.Alarm{LightOnly: true, Silent: true}, withDefault, 0},
	} {
		if got := tc.in.SunriseFor(tc.al); got != tc.want {
			t.Errorf("%+v with default %d: %d minutes, want %d", tc.al, tc.in.SunriseMinutes, got, tc.want)
		}
	}
}

// alarm_set_light_only and alarm_set turn the same alarm one way and the other, and alarm_set_silent
// turns light only off again.
func TestSettingLightOnlyFromHomeAssistant(t *testing.T) {
	config.Use(filepath.Join(t.TempDir(), "state.json"))
	a := build()
	al, err := a.setFromHA(6, 30, config.DaysOnce, "Morning", "", alarmKind{lightOnly: true})
	if err != nil || !al.LightOnly || al.Silent {
		t.Fatalf("%+v %v", al, err)
	}
	src := a.sources(time.Now())
	if len(src) != 1 || !src[0].lightOnly || src[0].sunrise != config.LightOnlyMinutes {
		t.Errorf("sources %+v", src)
	}
	al, err = a.setFromHA(6, 30, config.DaysOnce, "Morning", "", alarmKind{silent: true})
	if err != nil || al.LightOnly || !al.Silent {
		t.Errorf("%+v %v", al, err)
	}
	al, err = a.setFromHA(6, 30, config.DaysOnce, "Morning", "", alarmKind{})
	if err != nil || al.LightOnly || al.Silent || len(config.Get().Alarms.List) != 1 {
		t.Errorf("%+v %v, %d alarms", al, err, len(config.Get().Alarms.List))
	}
}

// An alarm saved before light only existed loads as an alarm that rings.
func TestAnOldAlarmLoadsAsOneThatRings(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	if err := os.WriteFile(path, []byte(`{"alarms":{"list":[{"id":"a","hour":7,"minute":0,"on":true,"sunrise":20}]}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	config.Use(path)
	list := config.Get().Alarms.List
	if len(list) != 1 || list[0].LightOnly || list[0].Silent || config.Get().Alarms.SunriseFor(list[0]) != 20 {
		t.Errorf("loaded %+v", list)
	}
}

// A light-only alarm's time does not drop the light, nor the lamps following it: it stays at full for
// lightOnlyGlow, then goes; a stop on the device lets it go sooner. Its time also tells the screen
// (Changed), which ends the night on it and on the Night mode switch at once.
func TestALightOnlyAlarmHoldsItsLight(t *testing.T) {
	config.Use(filepath.Join(t.TempDir(), "state.json"))
	events(t)
	a := build()
	endRing(t, a)
	changed := make(chan struct{}, 8)
	a.Changed.Listen(func(struct{}) {
		select {
		case changed <- struct{}{}:
		default:
		}
	})
	now := time.Now()
	a.fire(source{key: "wake", lightOnly: true, sunrise: 20}, now)
	select {
	case <-changed:
	case <-time.After(time.Second):
		t.Error("the screen was not told the night ended")
	}

	for _, after := range []time.Duration{0, time.Minute, lightOnlyGlow - time.Minute} {
		if got := a.SunriseProgress(now.Add(after)); got != 1 {
			t.Errorf("%v after a light-only alarm the light is at %.2f, want full", after, got)
		}
	}
	if until, snooze, ok := a.WakeHold(now.Add(time.Minute)); !ok || snooze || !until.Equal(now.Add(lightOnlyGlow)) {
		t.Errorf("hold %v snooze %v ok %v, want until %v", until, snooze, ok, now.Add(lightOnlyGlow))
	}
	if until, ok := a.LightOnlyGlow(now.Add(time.Minute)); !ok || !until.Equal(now.Add(lightOnlyGlow)) {
		t.Errorf("glow %v %v", until, ok)
	}
	if got := a.SunriseProgress(now.Add(lightOnlyGlow + time.Second)); got != 0 {
		t.Errorf("the light is at %.2f after its half hour, want off", got)
	}

	if !a.LetGoOfLight() {
		t.Error("a stop found nothing holding the light")
	}
	if got := a.SunriseProgress(now.Add(time.Minute)); got != 0 {
		t.Errorf("the light is at %.2f after a stop, want off", got)
	}
	if a.LetGoOfLight() {
		t.Error("a second stop found the light still held")
	}
}

// An alarm that rings during the light a light-only alarm left up, and is stopped, ends that light as
// well; one snoozed leaves it. Stop in Home Assistant ends it whether or not something is sounding.
func TestAStoppedRingEndsTheLightOnlyLight(t *testing.T) {
	config.Use(filepath.Join(t.TempDir(), "state.json"))
	events(t)
	a := build()
	endRing(t, a)
	glowing := func() bool { _, ok := a.LightOnlyGlow(time.Now()); return ok }

	a.fire(source{key: "light", lightOnly: true, sunrise: 20}, time.Now())
	a.mu.Lock()
	a.ringing = &Ring{Key: "wake", At: time.Now()}
	a.silence = func() {}
	a.mu.Unlock()
	if !a.SnoozeFor(9) {
		t.Fatal("nothing to snooze")
	}
	a.rang()
	if !glowing() {
		t.Error("a snooze let go of the light a light-only alarm left up")
	}
	a.CancelSnoozes()

	a.mu.Lock()
	a.ringing = &Ring{Key: "wake", At: time.Now()}
	a.mu.Unlock()
	a.rang()
	if glowing() {
		t.Error("a stopped ring left the light a light-only alarm left up")
	}

	// Stop in Home Assistant, with an alarm sounding.
	a.fire(source{key: "light", lightOnly: true, sunrise: 20}, time.Now())
	a.fire(source{key: "wake", label: "Wake up"}, time.Now())
	if !ring.IsSounding() {
		t.Fatal("the alarm is not sounding")
	}
	a.stop.OnPress()
	if glowing() {
		t.Error("Stop in Home Assistant left the light a light-only alarm left up")
	}
}

// Turning a light-only alarm off, or deleting it, lets go of the light it left up, as it lets go of a
// light held for it; another alarm turned off does not.
func TestTurningALightOnlyAlarmOffEndsItsLight(t *testing.T) {
	config.Use(filepath.Join(t.TempDir(), "state.json"))
	events(t)
	a := build()
	endRing(t, a)
	glowing := func() bool { _, ok := a.LightOnlyGlow(time.Now()); return ok }

	light, err := a.Set(6, 30, config.DaysEvery, "Light")
	if err != nil {
		t.Fatal(err)
	}
	light.LightOnly = true
	if err := a.Put(light); err != nil {
		t.Fatal(err)
	}
	other, err := a.Set(7, 0, config.DaysEvery, "Other")
	if err != nil {
		t.Fatal(err)
	}

	a.fire(source{key: light.ID, lightOnly: true, sunrise: 20, local: true, days: config.DaysEvery}, time.Now())
	other.On = false
	if err := a.Put(other); err != nil {
		t.Fatal(err)
	}
	if err := a.Delete(other.ID); err != nil {
		t.Fatal(err)
	}
	if !glowing() {
		t.Fatal("another alarm turned off and deleted let go of the light")
	}
	light.On = false
	if err := a.Put(light); err != nil {
		t.Fatal(err)
	}
	if glowing() {
		t.Error("the light-only alarm turned off left its light up")
	}

	light.On = true
	if err := a.Put(light); err != nil {
		t.Fatal(err)
	}
	a.fire(source{key: light.ID, lightOnly: true, sunrise: 20, local: true, days: config.DaysEvery}, time.Now())
	if err := a.Delete(light.ID); err != nil {
		t.Fatal(err)
	}
	if glowing() {
		t.Error("the light-only alarm deleted left its light up")
	}
}
