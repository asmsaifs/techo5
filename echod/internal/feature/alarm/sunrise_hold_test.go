package alarm

import (
	"math"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/HuskerMinion/techo5/echod/internal/config"
)

// A morning as the scheduler lives it, one step at a time: what rings, what is snoozed and stopped,
// and how far the wake light is at each moment (#161). The state is the device's own (a.ringing,
// a.snoozed, a.bridge); only the sound is left out.
type lightStep struct {
	at   string  // "8:02", or "8:02:30"
	do   string  // "ring <key>", "snooze <minutes>", "stop", "drop <key>", or nothing to only look
	want float64 // SunriseProgress after the step
}

func playMorning(t *testing.T, sources []source, steps []lightStep) {
	t.Helper()
	a := &Alarms{}
	day := time.Date(2026, 10, 16, 0, 0, 0, 0, time.UTC)
	all := func() []source { return append(slices.Clone(sources), a.snoozed...) }
	for _, st := range steps {
		layout := "15:04"
		if strings.Count(st.at, ":") == 2 {
			layout = "15:04:05"
		}
		clock, err := time.Parse(layout, st.at)
		if err != nil {
			t.Fatal(err)
		}
		now := day.Add(time.Duration(clock.Hour())*time.Hour + time.Duration(clock.Minute())*time.Minute + time.Duration(clock.Second())*time.Second)

		verb, arg, _ := strings.Cut(st.do, " ")
		switch verb {
		case "ring": // as fire does it, snooze or alarm, alone or folded into what rings already
			i := slices.IndexFunc(all(), func(s source) bool { return s.key == arg })
			if i < 0 {
				t.Fatalf("%s: nothing to ring called %q", st.at, arg)
			}
			s := all()[i]
			a.snoozed = slices.DeleteFunc(a.snoozed, func(x source) bool { return x.key == s.key })
			if a.ringing != nil {
				a.ringing.lit = a.ringing.lit || lights(s)
				break
			}
			a.ringing = &Ring{Key: strings.TrimPrefix(s.key, "snooze:"), At: now, lit: lights(s)}
		case "snooze": // as SnoozeFor and rang do it
			minutes, _ := strconv.Atoi(arg)
			r := a.ringing
			a.snoozed = append(a.snoozed, source{key: "snooze:" + r.Key, once: now.Add(time.Duration(minutes) * time.Minute), lit: r.lit})
			a.ringing = nil
		case "stop": // as rang does it
			r := a.ringing
			a.ringing = nil
			if r.lit {
				a.endLight(all(), now)
			}
		case "drop": // turned off or deleted
			sources = slices.DeleteFunc(sources, func(s source) bool { return s.key == arg })
		}
		if got := a.lightNow(all(), now); math.Abs(got-st.want) > 0.001 {
			t.Errorf("%s %s: the light is %.3f of the way, want %.3f", st.at, st.do, got, st.want)
		}
	}
}

func TestTheWakeLightHoldsAfterTheAlarm(t *testing.T) {
	every := func(key string, hour, minute, window int) source {
		return source{key: key, hour: hour, min: minute, days: config.DaysEvery, local: true, sunrise: window}
	}
	helper := func(entity, state string, window int) source {
		s, ok := fromHelper(entity, state, time.UTC)
		if !ok {
			t.Fatalf("helper state %q not read", state)
		}
		s.sunrise = window
		return s
	}

	for _, tc := range []struct {
		name    string
		sources []source
		steps   []lightStep
	}{
		{
			// The report: half an hour of light each, the second alarm's ramp starting as the first
			// rings. It used to drop to the start of the 8:30 ramp at 8:00.
			name:    "8:00 and 8:30, half an hour each",
			sources: []source{every("a", 8, 0, 30), every("b", 8, 30, 30)},
			steps: []lightStep{
				{at: "7:45", want: 0.5},
				{at: "8:00", do: "ring a", want: 1},
				{at: "8:02", do: "stop", want: 1},
				{at: "8:15", want: 1},
				{at: "8:30", do: "ring b", want: 1},
				{at: "8:31", do: "stop", want: 0},
				{at: "9:00", want: 0},
			},
		},
		{
			name:    "the windows touch: stopped the moment it rang",
			sources: []source{every("a", 8, 0, 30), every("b", 8, 30, 30)},
			steps: []lightStep{
				{at: "8:00", do: "ring a", want: 1},
				{at: "8:00", do: "stop", want: 1},
				{at: "8:29:59", want: 1},
			},
		},
		{
			name:    "8:00 and 8:20",
			sources: []source{every("a", 8, 0, 30), every("b", 8, 20, 30)},
			steps: []lightStep{
				{at: "8:00", do: "ring a", want: 1},
				{at: "8:01", do: "stop", want: 1},
				{at: "8:20", do: "ring b", want: 1},
				{at: "8:21", do: "stop", want: 0},
			},
		},
		{
			// Rung together, they are one ring, and lit if either has a light.
			name:    "folded into a ring with no light of its own",
			sources: []source{every("a", 8, 0, 0), every("b", 8, 0, 20)},
			steps: []lightStep{
				{at: "7:50", want: 0.5},
				{at: "8:00", do: "ring a", want: 0},
				{at: "8:00", do: "ring b", want: 1},
				{at: "8:01", do: "stop", want: 0},
			},
		},
		{
			name:    "a stop with nothing ahead ends the light",
			sources: []source{every("a", 8, 0, 30)},
			steps: []lightStep{
				{at: "7:30", want: 0},
				{at: "8:00", do: "ring a", want: 1},
				{at: "8:03", do: "stop", want: 0},
			},
		},
		{
			// The next alarm's ramp is its own once its window opens, as before.
			name:    "the next window not open yet",
			sources: []source{every("a", 8, 0, 30), every("b", 8, 45, 10)},
			steps: []lightStep{
				{at: "8:00", do: "ring a", want: 1},
				{at: "8:05", do: "stop", want: 0},
				{at: "8:40", want: 0.5},
			},
		},
		{
			name:    "an alarm with no light does not ring the light up",
			sources: []source{every("c", 8, 10, 0)},
			steps: []lightStep{
				{at: "8:10", do: "ring c", want: 0},
				{at: "8:11", do: "stop", want: 0},
			},
		},
		{
			// An alarm with no light between the two neither holds the light nor ends it.
			name:    "an alarm with no light in the middle",
			sources: []source{every("a", 8, 0, 30), every("c", 8, 10, 0), every("b", 8, 30, 30)},
			steps: []lightStep{
				{at: "8:00", do: "ring a", want: 1},
				{at: "8:01", do: "stop", want: 1},
				{at: "8:10", do: "ring c", want: 1},
				{at: "8:11", do: "stop", want: 1},
				{at: "8:30", do: "ring b", want: 1},
				{at: "8:31", do: "stop", want: 0},
			},
		},
		{
			name:    "the alarm held for is turned off",
			sources: []source{every("a", 8, 0, 30), every("b", 8, 30, 30)},
			steps: []lightStep{
				{at: "8:00", do: "ring a", want: 1},
				{at: "8:01", do: "stop", want: 1},
				{at: "8:05", do: "drop b", want: 0},
			},
		},
		{
			name:    "a snooze holds the light until its ring is stopped",
			sources: []source{every("a", 8, 0, 30)},
			steps: []lightStep{
				{at: "8:00", do: "ring a", want: 1},
				{at: "8:01", do: "snooze 9", want: 1},
				{at: "8:09", want: 1},
				{at: "8:10", do: "ring snooze:a", want: 1},
				{at: "8:10", do: "snooze 9", want: 1},
				{at: "8:19", do: "ring snooze:a", want: 1},
				{at: "8:20", do: "stop", want: 0},
			},
		},
		{
			// 8:00 and 9:05 with half an hour of light: one 9-minute snooze stops at 8:10, before the
			// 9:05 window opens at 8:35, so the light ends and the 9:05 ramp comes up as usual.
			name:    "a snooze that does not reach the next window",
			sources: []source{every("a", 8, 0, 30), every("b", 9, 5, 30)},
			steps: []lightStep{
				{at: "8:00", do: "ring a", want: 1},
				{at: "8:00", do: "snooze 9", want: 1},
				{at: "8:09", do: "ring snooze:a", want: 1},
				{at: "8:10", do: "stop", want: 0},
				{at: "8:50", want: 0.5},
			},
		},
		{
			// Snoozed until its ring falls inside the 9:05 window, it bridges to 9:05.
			name:    "snoozes that bridge to the next alarm",
			sources: []source{every("a", 8, 0, 30), every("b", 9, 5, 30)},
			steps: []lightStep{
				{at: "8:00", do: "ring a", want: 1},
				{at: "8:00", do: "snooze 9", want: 1},
				{at: "8:09", do: "ring snooze:a", want: 1},
				{at: "8:09", do: "snooze 9", want: 1},
				{at: "8:18", do: "ring snooze:a", want: 1},
				{at: "8:18", do: "snooze 9", want: 1},
				{at: "8:27", do: "ring snooze:a", want: 1},
				{at: "8:27", do: "snooze 9", want: 1},
				{at: "8:36", do: "ring snooze:a", want: 1},
				{at: "8:37", do: "stop", want: 1},
				{at: "9:00", want: 1},
				{at: "9:05", do: "ring b", want: 1},
				{at: "9:06", do: "stop", want: 0},
			},
		},
		{
			name:    "a snooze of an alarm with no light lights nothing",
			sources: []source{every("c", 8, 0, 0)},
			steps: []lightStep{
				{at: "8:00", do: "ring c", want: 0},
				{at: "8:00", do: "snooze 9", want: 0},
				{at: "8:05", want: 0},
			},
		},
		{
			// A Home Assistant helper is an alarm like any other here, either way round.
			name:    "a device alarm then a helper",
			sources: []source{every("a", 8, 0, 30), helper("input_datetime.wake", "08:20:00", 30)},
			steps: []lightStep{
				{at: "8:00", do: "ring a", want: 1},
				{at: "8:01", do: "stop", want: 1},
				{at: "8:20", do: "ring input_datetime.wake", want: 1},
				{at: "8:21", do: "stop", want: 0},
			},
		},
		{
			name:    "a dated helper then a device alarm",
			sources: []source{helper("input_datetime.wake", "2026-10-16 08:00:00", 30), every("b", 8, 30, 30)},
			steps: []lightStep{
				{at: "7:45", want: 0.5},
				{at: "8:00", do: "ring input_datetime.wake", want: 1},
				{at: "8:00", do: "snooze 9", want: 1},
				{at: "8:09", do: "ring snooze:input_datetime.wake", want: 1},
				{at: "8:10", do: "stop", want: 1},
				{at: "8:30", do: "ring b", want: 1},
				{at: "8:31", do: "stop", want: 0},
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) { playMorning(t, tc.sources, tc.steps) })
	}
}

// The bridge holds a moment past the alarm's time, for the scheduler to ring it, and no longer.
func TestTheBridgeLetsGoAfterItsAlarm(t *testing.T) {
	at := time.Date(2026, 10, 16, 8, 30, 0, 0, time.UTC)
	s := []source{{key: "b", hour: 8, min: 30, days: config.DaysEvery, sunrise: 30}}
	b := bridge{key: "b", at: at}
	if !b.holds(s, at.Add(30*time.Second)) {
		t.Error("the light dropped in the moment before the alarm rang")
	}
	if b.holds(s, at.Add(2*time.Minute)) {
		t.Error("a bridge held past its alarm")
	}
	moved := []source{{key: "b", hour: 8, min: 45, days: config.DaysEvery, sunrise: 30}}
	if b.holds(moved, at.Add(-10*time.Minute)) {
		t.Error("a bridge held for an alarm moved to another time")
	}
}

// On the device itself: a snooze of a lit ring holds the light, and keeps holding it after a restart;
// a lit ring that ends inside an alarm's window holds the light for it.
func TestTheDeviceHoldsTheWakeLight(t *testing.T) {
	config.Use(filepath.Join(t.TempDir(), "state.json"))
	a := build()

	a.mu.Lock()
	a.ringing = &Ring{Key: "wake", Label: "Wake up", At: time.Now(), lit: true}
	a.silence = func() {}
	a.mu.Unlock()
	if got := a.SunriseProgress(time.Now()); got != 1 {
		t.Errorf("while a lit alarm rings the light is %.2f, want full", got)
	}
	if !a.SnoozeFor(9) {
		t.Fatal("nothing to snooze")
	}
	a.rang()
	if got := a.SunriseProgress(time.Now()); got != 1 {
		t.Errorf("while it is snoozed the light is %.2f, want full", got)
	}
	if s := config.Get().Alarms.Snoozed; len(s) != 1 || !s[0].Lit {
		t.Errorf("the saved snooze is %+v, want one that keeps the light", s)
	}
	again := build()
	again.Restore(config.Get())
	if got := again.SunriseProgress(time.Now()); got != 1 {
		t.Errorf("after a restart the snoozed light is %.2f, want full", got)
	}
	a.CancelSnoozes()
	if got := a.SunriseProgress(time.Now()); got != 0 {
		t.Errorf("with the snooze canceled the light is %.2f, want off", got)
	}

	// An alarm twenty minutes off with half an hour of light: its window is open as the ring ends.
	next := time.Now().Add(20 * time.Minute)
	al, err := a.Set(next.Hour(), next.Minute(), config.DaysEvery, "")
	if err != nil {
		t.Fatal(err)
	}
	al.Sunrise = 30
	if err := a.Put(al); err != nil {
		t.Fatal(err)
	}
	a.mu.Lock()
	a.ringing = &Ring{Key: "wake", At: time.Now(), lit: true}
	a.mu.Unlock()
	a.rang()
	if got := a.SunriseProgress(time.Now()); got != 1 {
		t.Errorf("after a stop inside the next alarm's window the light is %.2f, want full", got)
	}
	if err := a.Delete(al.ID); err != nil {
		t.Fatal(err)
	}
	if got := a.SunriseProgress(time.Now()); got != 0 {
		t.Errorf("with that alarm deleted the light is %.2f, want off", got)
	}
}

// Stop from Home Assistant with nothing ringing lets go of the light held for the next alarm, as it
// drops the snoozes: whoever presses it wants the light off.
func TestStopWithNothingRingingLetsTheLightGo(t *testing.T) {
	config.Use(filepath.Join(t.TempDir(), "state.json"))
	a := build()

	next := time.Now().Add(20 * time.Minute)
	al, err := a.Set(next.Hour(), next.Minute(), config.DaysEvery, "")
	if err != nil {
		t.Fatal(err)
	}
	al.Sunrise = 30
	if err := a.Put(al); err != nil {
		t.Fatal(err)
	}
	a.mu.Lock()
	a.ringing = &Ring{Key: "wake", At: time.Now(), lit: true}
	a.mu.Unlock()
	a.rang()
	if got := a.SunriseProgress(time.Now()); got != 1 {
		t.Fatalf("after the ring the light is %.2f, want it held at full", got)
	}

	a.stop.OnPress()
	got := a.SunriseProgress(time.Now())
	if got >= 1 {
		t.Errorf("after Stop the light is still held at full")
	}
	// The next alarm's own ramp is untouched: twenty minutes out of thirty is a third of the way.
	if got < 0.3 || got > 0.37 {
		t.Errorf("after Stop the light is %.2f, want the next alarm's own ramp", got)
	}
}

// What holds the light is said on the screen, so it has to name the right thing.
func TestWhatHoldsTheLight(t *testing.T) {
	now := time.Date(2026, 10, 16, 8, 5, 0, 0, time.UTC)
	next := source{key: "b", hour: 8, min: 30, days: config.DaysEvery, sunrise: 30}
	snoozes := []source{
		{key: "snooze:a", once: now.Add(9 * time.Minute), lit: true},
		{key: "snooze:c", once: now.Add(4 * time.Minute), lit: true},
		{key: "snooze:d", once: now.Add(2 * time.Minute)}, // no light of its own
	}
	for _, tc := range []struct {
		name    string
		sources []source
		hold    wakeHold
		until   time.Time
		snooze  bool
		ok      bool
	}{
		{"nothing", []source{next}, wakeHold{}, time.Time{}, false, false},
		{"a ring", []source{next}, wakeHold{ringing: true}, time.Time{}, false, true},
		{"a bridge", []source{next}, wakeHold{bridge: bridge{key: "b", at: now.Add(25 * time.Minute)}}, now.Add(25 * time.Minute), false, true},
		{"the sooner lit snooze", append([]source{next}, snoozes...), wakeHold{}, now.Add(4 * time.Minute), true, true},
	} {
		until, snooze, ok := heldBy(tc.sources, tc.hold, now)
		if !until.Equal(tc.until) || snooze != tc.snooze || ok != tc.ok {
			t.Errorf("%s: held until %v (snooze %v, %v), want %v (snooze %v, %v)",
				tc.name, until, snooze, ok, tc.until, tc.snooze, tc.ok)
		}
	}
}

// A stop on the device with nothing sounding lets go of the light a lit snooze holds, and leaves the
// snooze to ring as it would have (#161); and of the light held for the next alarm, saved or not.
func TestADeviceStopLetsTheHeldLightGo(t *testing.T) {
	config.Use(filepath.Join(t.TempDir(), "state.json"))
	a := build()

	a.mu.Lock()
	a.ringing = &Ring{Key: "wake", Label: "Wake up", At: time.Now(), lit: true}
	a.silence = func() {}
	a.mu.Unlock()
	if !a.SnoozeFor(9) {
		t.Fatal("nothing to snooze")
	}
	a.rang()
	if got := a.SunriseProgress(time.Now()); got != 1 {
		t.Fatalf("while it is snoozed the light is %.2f, want full", got)
	}
	if !a.LetGoOfLight() {
		t.Fatal("a stop found no light held by the snooze")
	}
	if got := a.SunriseProgress(time.Now()); got != 0 {
		t.Errorf("after a stop the snoozed light is %.2f, want off", got)
	}
	if v := a.View(time.Now()); len(v.Snoozed) != 1 {
		t.Errorf("after a stop there are %d snoozes, want the one still coming", len(v.Snoozed))
	}
	if s := config.Get().Alarms.Snoozed; len(s) != 1 || s[0].Lit {
		t.Errorf("the saved snooze is %+v, want it kept without the light", s)
	}
	if a.LetGoOfLight() {
		t.Error("a second stop found the light still held")
	}
	a.CancelSnoozes()

	// The light held for an alarm twenty minutes off, with half an hour of light.
	next := time.Now().Add(20 * time.Minute)
	al, err := a.Set(next.Hour(), next.Minute(), config.DaysEvery, "")
	if err != nil {
		t.Fatal(err)
	}
	al.Sunrise = 30
	if err := a.Put(al); err != nil {
		t.Fatal(err)
	}
	a.mu.Lock()
	a.ringing = &Ring{Key: "wake", At: time.Now(), lit: true}
	a.mu.Unlock()
	a.rang()
	if h := config.Get().Alarms.Held; h == nil || h.Key != al.ID {
		t.Fatalf("the held light was saved as %+v, want the next alarm", h)
	}
	if !a.LetGoOfLight() {
		t.Fatal("a stop found no light held for the next alarm")
	}
	if got := a.SunriseProgress(time.Now()); got >= 1 {
		t.Error("after a stop the light is still held at full")
	}
	if h := config.Get().Alarms.Held; h != nil {
		t.Errorf("after a stop the saved held light is %+v, want none", h)
	}
}

// The light held for the next alarm survives a restart while it still holds, and not otherwise.
func TestTheHeldLightComesBackAfterARestart(t *testing.T) {
	config.Use(filepath.Join(t.TempDir(), "state.json"))
	a := build()
	next := time.Now().Add(20 * time.Minute)
	al, err := a.Set(next.Hour(), next.Minute(), config.DaysEvery, "")
	if err != nil {
		t.Fatal(err)
	}
	al.Sunrise = 30
	if err := a.Put(al); err != nil {
		t.Fatal(err)
	}
	a.mu.Lock()
	a.ringing = &Ring{Key: "wake", At: time.Now(), lit: true}
	a.mu.Unlock()
	a.rang()

	again := build()
	again.Restore(config.Get())
	if got := again.SunriseProgress(time.Now()); got != 1 {
		t.Errorf("after a restart the held light is %.2f, want full", got)
	}

	// Saved for an alarm since turned off, it does not come back, and the saved copy goes.
	al.On = false
	if err := config.Set().Alarms().Put(al); err != nil {
		t.Fatal(err)
	}
	third := build()
	third.Restore(config.Get())
	if got := third.SunriseProgress(time.Now()); got != 0 {
		t.Errorf("after a restart with that alarm off the light is %.2f, want off", got)
	}
	if h := config.Get().Alarms.Held; h != nil {
		t.Errorf("a held light that no longer holds is still saved: %+v", h)
	}

	// A saved config from before there was a held light restores with nothing held.
	if err := config.Set().Alarms().Held(nil); err != nil {
		t.Fatal(err)
	}
	fourth := build()
	fourth.Restore(config.Get())
	if fourth.bridge.key != "" {
		t.Errorf("with nothing saved the light is held for %q", fourth.bridge.key)
	}
}

// A clock set back hours does not hold the light for all of them: the bridged alarm's window has to
// be open still.
func TestTheBridgeLetsGoWhenTheClockGoesBack(t *testing.T) {
	at := time.Date(2026, 10, 16, 8, 30, 0, 0, time.UTC)
	s := []source{{key: "b", hour: 8, min: 30, days: config.DaysEvery, sunrise: 30}}
	b := bridge{key: "b", at: at}
	if !b.holds(s, at.Add(-30*time.Minute)) {
		t.Error("the bridge let go with its alarm's window open")
	}
	if b.holds(s, at.Add(-3*time.Hour)) {
		t.Error("with the clock three hours back the bridge still holds")
	}
}
