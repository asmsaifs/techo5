//go:build !dot && !spot

package display

import (
	"fmt"
	"image"
	"testing"
	"time"

	esphome "github.com/ygelfand/go-esphome-device"

	"github.com/HuskerMinion/techo5/echod/internal/config"
	"github.com/HuskerMinion/techo5/echod/internal/feature/voice"
	"github.com/HuskerMinion/techo5/echod/internal/hardware/screen"
	"github.com/HuskerMinion/techo5/echod/internal/hardware/touch"
)

// runAction runs one of the display's actions by its name, as Home Assistant would.
func runAction(t *testing.T, d *Display, name string) error {
	t.Helper()
	for _, a := range d.Actions() {
		if a.Name == name {
			_, err := a.Run(esphome.Call{})
			return err
		}
	}
	t.Fatalf("no %s action", name)
	return nil
}

// weather_show puts the forecast up and radar_show the rain map, each over the other, as the page's
// own button turns between them.
func TestWeatherShowAndRadarShow(t *testing.T) {
	d := nightTestDisplay(t)
	if err := runAction(t, d, "weather_show"); err != nil {
		t.Fatalf("weather_show: %v", err)
	}
	if !time.Now().Before(d.weatherUntil) || d.radar {
		t.Fatalf("weather_show: up until %v, radar %v", d.weatherUntil, d.radar)
	}
	if err := runAction(t, d, "radar_show"); err != nil {
		t.Fatalf("radar_show: %v", err)
	}
	if !time.Now().Before(d.weatherUntil) || !d.radar {
		t.Fatalf("radar_show: up until %v, radar %v", d.weatherUntil, d.radar)
	}
	if err := runAction(t, d, "weather_show"); err != nil || d.radar {
		t.Errorf("weather_show over the rain map: %v, radar %v", err, d.radar)
	}
}

// With the weather set to none, neither opens, and Home Assistant is told why.
func TestNoWeatherNoWeatherShow(t *testing.T) {
	d := nightTestDisplay(t)
	if err := config.Set().Home().Weather(config.WeatherOff); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"weather_show", "radar_show"} {
		if err := runAction(t, d, name); err == nil {
			t.Errorf("%s worked with no weather", name)
		}
	}
	if !d.weatherUntil.IsZero() || d.radar {
		t.Errorf("the weather page went up anyway: until %v, radar %v", d.weatherUntil, d.radar)
	}
}

// At night, weather_show and radar_show on a screen the night put out light it for the page, at the
// night light's level when one is set and at the night's half brightness when not, and the night
// leaves it lit while the page is up: the action no longer works on a screen nobody can see. Once the
// page has gone, the night has the screen back.
func TestWeatherShowLiftsTheNight(t *testing.T) {
	for _, nightLight := range []bool{true, false} {
		d := nightTestDisplay(t)
		d.view = voice.State{Phase: "idle"}
		d.light = &esphome.Light{
			Base:                esphome.Base{ObjectID: "screen", Name: "Screen", Icon: "mdi:monitor"},
			SupportedColorModes: []esphome.ColorMode{esphome.ColorModeBrightness},
		}
		h := time.Now().Hour()
		if err := config.Set().Screen().Night(fmt.Sprintf("%d-%d", (h+23)%24, (h+2)%24)); err != nil {
			t.Fatal(err)
		}
		if err := config.Set().Screen().NightLight(nightLight); err != nil {
			t.Fatal(err)
		}
		d.touchedAt = time.Now().Add(-time.Hour)
		d.on, d.nightDark, d.ceiling = false, true, 100

		if err := runAction(t, d, "radar_show"); err != nil {
			t.Fatalf("night light %v: radar_show: %v", nightLight, err)
		}
		d.mu.Lock()
		on, dark, glow, asked, level := d.on, d.nightDark, d.nightGlow, d.askedUp(time.Now()), d.level
		d.mu.Unlock()
		if !on || dark || !asked || glow != nightLight {
			t.Fatalf("night light %v: on %v, dark %v, glowing %v, asked %v", nightLight, on, dark, glow, asked)
		}
		if !nightLight && level > screen.BacklightMax/2 {
			t.Errorf("lit at %v at night, over half of %v", level, screen.BacklightMax)
		}
		d.night(time.Now(), true, d.view)
		if d.nightDark || !d.on {
			t.Errorf("night light %v: the night put the screen out under the rain map", nightLight)
		}

		// Put away: the night has it again.
		d.mu.Lock()
		d.weatherUntil = time.Time{}
		d.mu.Unlock()
		d.night(time.Now(), true, d.view)
		if !nightLight && !d.nightDark {
			t.Errorf("the screen stayed lit after the rain map went")
		}
	}
}

// By day, and on a screen somebody brought up at night, weather_show changes nothing about the light.
func TestWeatherShowLeavesALitScreen(t *testing.T) {
	d := nightTestDisplay(t)
	h := time.Now().Hour()
	if err := config.Set().Screen().Night(fmt.Sprintf("%d-%d", (h+23)%24, (h+2)%24)); err != nil {
		t.Fatal(err)
	}
	d.on = true
	if err := runAction(t, d, "weather_show"); err != nil {
		t.Fatal(err)
	}
	if d.askedNight {
		t.Errorf("a screen already up was taken over for the night")
	}
}

// A screen somebody turned off at night is not lit by weather_show: with a night light set, it would
// stay lit at that level all night. The action still works, the page waiting under the dark screen.
func TestWeatherShowLeavesAScreenTurnedOff(t *testing.T) {
	for _, nightLight := range []bool{true, false} {
		d := nightTestDisplay(t)
		d.view = voice.State{Phase: "idle"}
		h := time.Now().Hour()
		if err := config.Set().Screen().Night(fmt.Sprintf("%d-%d", (h+23)%24, (h+2)%24)); err != nil {
			t.Fatal(err)
		}
		if err := config.Set().Screen().NightLight(nightLight); err != nil {
			t.Fatal(err)
		}
		d.touchedAt = time.Now().Add(-time.Hour)
		d.on, d.nightDark, d.ceiling = false, false, 100

		if err := runAction(t, d, "weather_show"); err != nil {
			t.Fatalf("night light %v: weather_show: %v", nightLight, err)
		}
		d.mu.Lock()
		on, glow, asked, until := d.on, d.nightGlow, d.askedNight, d.weatherUntil
		d.mu.Unlock()
		if on || glow || asked {
			t.Errorf("night light %v: a screen turned off was lit: on %v, glowing %v, held %v", nightLight, on, glow, asked)
		}
		if !time.Now().Before(until) {
			t.Errorf("night light %v: the forecast did not open", nightLight)
		}
		d.night(time.Now(), false, d.view)
		if d.on || d.nightGlow {
			t.Errorf("night light %v: the night lit a screen turned off", nightLight)
		}
	}
}

// On the night light, the forecast Home Assistant asked for takes its taps as by day: the Radar button
// turns it to the rain map and a tap anywhere else puts it away, the night light staying. With a tap on
// the clock opening the dashboard, that tap is the page's, not the night dashboard's.
func TestTapsReachAPageAskedForAtNight(t *testing.T) {
	d := nightTestDisplay(t)
	h := time.Now().Hour()
	if err := config.Set().Screen().Night(fmt.Sprintf("%d-%d", (h+23)%24, (h+2)%24)); err != nil {
		t.Fatal(err)
	}
	for _, err := range []error{
		config.Set().Screen().Welcomed(true),
		config.Set().Screen().NightLight(true),
		config.Set().Dashboard().Mode(config.DashboardDrawn),
		config.Set().Screen().ClockTap("dashboard"),
	} {
		if err != nil {
			t.Fatal(err)
		}
	}
	d.r = testRenderer()
	d.view = voice.State{Phase: "idle"}
	d.on, d.nightGlow = true, true
	d.ShowWeather(false)
	d.askedNight = true
	if !d.overNight() {
		t.Fatal("a page asked for at night does not count as something over the night")
	}

	b := d.r.weatherButton()
	d.gesture(touch.Gesture{Kind: touch.Tap, X: (b.Min.X + b.Max.X) / 2, Y: (b.Min.Y + b.Max.Y) / 2})
	if !d.radar || !time.Now().Before(d.weatherUntil) {
		t.Fatalf("the Radar button on the night light: radar %v, up until %v", d.radar, d.weatherUntil)
	}
	tapAt(d, image.Pt(480, 240))
	if time.Now().Before(d.weatherUntil) {
		t.Error("a tap on the rain map on the night light did not put it away")
	}
	if d.dash {
		t.Error("a tap on the rain map opened the night dashboard over it")
	}
	if !d.nightGlow {
		t.Error("a tap on the page lifted the night light")
	}
}

// The night's hold on a page Home Assistant asked for lasts as long as the page: the Radar button
// keeping the rain map up past the page's first half minute, minutes on, keeps the hold too. Once the
// page has closed the hold is gone, and a forecast put up after it (a weather question) is not held.
func TestTheNightHoldLastsAsLongAsThePage(t *testing.T) {
	d := nightTestDisplay(t)
	now := time.Now()
	d.mu.Lock()
	defer d.mu.Unlock()
	d.askedNight, d.weatherUntil = true, now.Add(weatherShow)
	later := now.Add(10 * time.Minute)
	d.weatherUntil = later.Add(weatherShow) // the Radar button, pressed again and again
	if !d.askedUp(later) {
		t.Fatal("the hold ended while the page was still up")
	}
	if d.askedUp(later.Add(weatherShow)) || d.askedNight {
		t.Fatal("the hold outlasted the page")
	}
	d.weatherUntil = later.Add(2 * weatherShow)
	if d.askedUp(later.Add(weatherShow + time.Second)) {
		t.Error("a forecast put up after the page closed is held for the night")
	}
}

// Only taps reach the forecast asked for at night: a swipe from the top does not open the settings
// over the night, and a long press still lifts the night light.
func TestOnlyTapsReachAPageAskedForAtNight(t *testing.T) {
	d := nightTestDisplay(t)
	h := time.Now().Hour()
	if err := config.Set().Screen().Night(fmt.Sprintf("%d-%d", (h+23)%24, (h+2)%24)); err != nil {
		t.Fatal(err)
	}
	if err := config.Set().Screen().Welcomed(true); err != nil {
		t.Fatal(err)
	}
	d.r = testRenderer()
	d.view = voice.State{Phase: "idle"}
	d.on, d.nightGlow = true, true
	d.ShowWeather(false)
	d.askedNight = true

	d.gesture(touch.Gesture{Kind: touch.SwipeDown, X: 480, Y: 5})
	if d.sheet {
		t.Error("a swipe from the top opened the settings over the night")
	}
	d.gesture(touch.Gesture{Kind: touch.Hold, X: 480, Y: 240})
	if d.nightGlow {
		t.Error("a long press on the forecast did not lift the night light")
	}
}
