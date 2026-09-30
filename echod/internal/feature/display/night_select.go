//go:build !dot && !spot

package display

import (
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	esphome "github.com/ygelfand/go-esphome-device"

	"github.com/HuskerMinion/techo5/echod/internal/config"
)

// The night's two settings in Home Assistant, beside Screen and Auto brightness: its hours, and what
// it does to the screen. Both are on the screen too, under Display.

// nightHoursText is a window as Home Assistant lists it. In 24-hour time whatever the screen's clock
// says, so the list does not change under an automation when somebody changes the clock format.
func nightHoursText(v string) string {
	from, to, ok := nightWindow(v)
	if !ok {
		return "Never"
	}
	return quarterText(from) + " – " + quarterText(to)
}

// nightCustom is the Night hours choice for hours that are none of the presets, set with Night starts
// and Night ends.
const nightCustom = "Custom"

// nightByHA is the Night hours choice that leaves the night to Home Assistant's Night mode switch.
const nightByHA = "Controlled by Home Assistant"

// nightHoursLabel is what the Night hours select shows: the preset, Custom, or Home Assistant's.
func nightHoursLabel(v string) string {
	if config.Get().Screen.NightByHA {
		return nightByHA
	}
	for _, p := range nightPresets {
		if p == v {
			return nightHoursText(p)
		}
	}
	if _, _, ok := nightWindow(v); ok {
		return nightCustom
	}
	return nightHoursText("")
}

// quarterText is minutes since midnight as 24-hour time, "19:30".
func quarterText(m int) string { return fmt.Sprintf("%02d:%02d", m/60, m%60) }

// quarters are the times Night starts and Night ends offer: every quarter hour of the day.
var quarters = func() []string {
	out := make([]string, 0, 96)
	for m := 0; m < 24*60; m += 15 {
		out = append(out, quarterText(m))
	}
	return out
}()

func nightHoursSelect(d *Display) *esphome.Select {
	s := &esphome.Select{
		Base: esphome.Base{
			ObjectID: "screen_night_hours",
			Name:     "Night hours",
			Icon:     "mdi:weather-night",
			Category: esphome.CategoryConfig,
		},
	}
	for _, p := range nightPresets {
		s.Options = append(s.Options, nightHoursText(p))
	}
	s.Options = append(s.Options, nightCustom, nightByHA)
	s.OnCommand = func(v string) {
		if v == nightByHA {
			d.setNightByHA(true)
			return
		}
		for _, p := range nightPresets {
			if nightHoursText(p) == v {
				d.setNight(p)
				return
			}
		}
		// Custom goes back to the hours that are set; they are changed with Night starts and Night ends.
		d.setNightByHA(false)
	}
	return s
}

// nightNow is whether it is night: the hours, unless Home Assistant has the night (Night hours set to
// Controlled by Home Assistant), and the Night mode switch over either. With hours set, the switch
// holds until they next start or end the night, and no longer than a day; left to Home Assistant, it
// holds until it is turned again.
func nightNow(now time.Time) bool {
	sc := config.Get().Screen
	hours := !sc.NightByHA && config.InWindow(sc.Night, now)
	if sc.NightOverride != "on" && sc.NightOverride != "off" {
		return hours
	}
	if !sc.NightByHA {
		at := time.Unix(sc.NightOverrideAt, 0)
		if now.Before(at) || now.Sub(at) > 24*time.Hour || !now.Before(nextNightChange(sc.Night, at)) {
			return hours // the hours have started or ended the night since the switch was turned
		}
	}
	return sc.NightOverride == "on"
}

// nextNightChange is the first time after at that the hours start or end the night; a day on when
// there are no hours.
func nextNightChange(window string, at time.Time) time.Time {
	from, to, ok := config.ParseWindow(window)
	if !ok {
		return at.Add(24 * time.Hour)
	}
	next := at.Add(48 * time.Hour)
	for d := range 2 {
		for _, m := range []int{from, to} {
			// By the clock on the wall, as the hours are: midnight plus a duration is an hour off on the
			// two days a year the clocks change.
			t := time.Date(at.Year(), at.Month(), at.Day()+d, m/60, m%60, 0, 0, at.Location())
			if t.After(at) && t.Before(next) {
				next = t
			}
		}
	}
	return next
}

// nightModeSwitch is Night mode in Home Assistant: on while it is night, and turned on or off to start
// or end the night now, from an automation as easily as from the dashboard.
func nightModeSwitch(d *Display) *esphome.Switch {
	sw := &esphome.Switch{
		Base: esphome.Base{
			ObjectID: "screen_night_mode",
			Name:     "Night mode",
			Icon:     "mdi:weather-night",
		},
	}
	sw.OnCommand = func(on bool) {
		v := "off"
		if on {
			v = "on"
		}
		if err := config.Set().Screen().NightOverride(v, time.Now().Unix()); err != nil {
			slog.Error("saving night mode failed", "err", err)
			return
		}
		slog.Info("screen: night mode", "on", on)
		d.showNightMode(nightNow(time.Now()))
		d.wake()
	}
	return sw
}

// showNightMode keeps the Night mode switch saying whether it is night, which the hours change on
// their own.
func (d *Display) showNightMode(in bool) {
	if d.nightMode == nil {
		return
	}
	// The frame and the switch's own command both publish; one at a time, or the older answer can land
	// last and stay, since an unchanged answer is not sent again.
	d.nightPubMu.Lock()
	defer d.nightPubMu.Unlock()
	d.mu.Lock()
	changed := d.nightShown != in || !d.nightSeen
	d.nightShown, d.nightSeen = in, true
	d.mu.Unlock()
	if changed {
		d.nightMode.Set(in)
	}
}

// nightLeftToHA is the settings sheet choosing Controlled by Home Assistant.
func (d *Display) nightLeftToHA() { d.setNightByHA(true) }

// setNightByHA leaves the night to Home Assistant, or gives it back to the hours. Either way the
// switch starts over, off, so the night is what was just chosen.
func (d *Display) setNightByHA(v bool) {
	if err := config.Set().Screen().NightByHA(v); err != nil {
		slog.Error("saving the night hours failed", "err", err)
		return
	}
	if err := config.Set().Screen().NightOverride("", 0); err != nil {
		slog.Error("saving night mode failed", "err", err)
	}
	slog.Info("screen: night left to Home Assistant", "on", v)
	d.nightHoursChanged()
	d.showNightMode(nightNow(time.Now()))
	d.wake()
}

// nightEndSelect is Night starts (start true) or Night ends: one end of the night, to the quarter hour.
func nightEndSelect(d *Display, start bool) *esphome.Select {
	id, name, icon := "screen_night_end", "Night ends", "mdi:weather-sunset-up"
	if start {
		id, name, icon = "screen_night_start", "Night starts", "mdi:weather-sunset-down"
	}
	s := &esphome.Select{
		Base:    esphome.Base{ObjectID: id, Name: name, Icon: icon, Category: esphome.CategoryConfig},
		Options: quarters,
	}
	s.OnCommand = func(v string) {
		m, ok := quarterMinutes(v)
		if !ok {
			return
		}
		from, to := nightOrDefault()
		if start {
			from = m
		} else {
			to = m
		}
		if from == to {
			slog.Warn("night hours: the start and the end are the same time; not changed", "at", v)
			d.nightHoursChanged()
			return
		}
		d.setNight(config.FormatWindow(from, to))
	}
	return s
}

// quarterMinutes reads "19:30" back into minutes since midnight.
func quarterMinutes(v string) (int, bool) {
	for i, q := range quarters {
		if q == v {
			return i * 15, true
		}
	}
	return 0, false
}

// nightOrDefault is the night set, or 22:00 to 06:00 for a device with none, as the end to keep
// when only one end is being changed.
func nightOrDefault() (from, to int) {
	if f, t, ok := nightWindow(config.Get().Screen.Night); ok {
		return f, t
	}
	return 22 * 60, 6 * 60
}

// setNight saves the night hours, shows them in Home Assistant, and lets the screen take them up.
// Choosing hours takes the night back from Home Assistant.
func (d *Display) setNight(v string) {
	if err := config.Set().Screen().Night(v); err != nil {
		slog.Error("saving the night hours failed", "err", err)
		return
	}
	if config.Get().Screen.NightByHA {
		if err := config.Set().Screen().NightByHA(false); err != nil {
			slog.Error("saving the night hours failed", "err", err)
		}
	}
	slog.Info("screen: night hours", "hours", cmpOr(v, "never"))
	d.nightHoursChanged()
	d.wake()
}

// Actions: screen_night_hours sets the night to any start and end, "19:00" and "09:30"; both empty is
// no night.
func (d *Display) Actions() []*esphome.Action {
	return []*esphome.Action{{
		Name: "screen_night_hours",
		Args: []esphome.Arg{{Name: "start", Type: esphome.ArgString}, {Name: "end", Type: esphome.ArgString}},
		Run:  func(c esphome.Call) (any, error) { return nil, d.setNightHours(c.String("start"), c.String("end")) },
	}, {
		Name: "calendar_popup_sources",
		Args: []esphome.Arg{{Name: "calendars", Type: esphome.ArgString}},
		Run:  func(c esphome.Call) (any, error) { return nil, setPopupCalendars(c.String("calendars")) },
	}, {
		Name: "calendar_show",
		Run: func(esphome.Call) (any, error) {
			if !d.OpenCalendar() {
				return nil, errors.New("calendar: this device shows no calendar; choose one with calendar_sources")
			}
			return nil, nil
		},
	}}
}

// setNightHours is the action: the night from start to end, "19:00" and "09:30"; both empty is none.
func (d *Display) setNightHours(start, end string) error {
	start, end = strings.TrimSpace(start), strings.TrimSpace(end)
	if start == "" && end == "" {
		d.setNight("")
		return nil
	}
	from, to, ok := config.ParseWindow(start + "-" + end)
	if !ok {
		return fmt.Errorf("night hours: %q to %q is not two different times like 19:00 and 09:30", start, end)
	}
	d.setNight(config.FormatWindow(from, to))
	return nil
}

func atNightSelect(d *Display) *esphome.Select {
	s := &esphome.Select{
		Base: esphome.Base{
			ObjectID: "screen_at_night",
			Name:     "Screen at night",
			Icon:     "mdi:lightbulb-night-outline",
			Category: esphome.CategoryConfig,
		},
		Options: atNightOptions,
	}
	s.OnCommand = func(v string) {
		for i, o := range atNightOptions {
			if o == v {
				d.setAtNight(i)
				return
			}
		}
	}
	return s
}

func nightStyleSelect(d *Display) *esphome.Select {
	s := &esphome.Select{
		Base: esphome.Base{
			ObjectID: "screen_night_clock_style",
			Name:     "Night clock style",
			Icon:     "mdi:clock-digital",
			Category: esphome.CategoryConfig,
		},
		Options: nightStyleOptions,
	}
	s.OnCommand = func(v string) {
		for i, o := range nightStyleOptions {
			if o == v {
				d.setNightStyle(i)
				return
			}
		}
	}
	return s
}

// setNightStyle saves the night clock's look and shows it at once, so it can be chosen while looking.
func (d *Display) setNightStyle(i int) {
	if i < 0 || i >= len(nightStyles) {
		return
	}
	if err := config.Set().Screen().NightClockStyle(nightStyles[i]); err != nil {
		slog.Error("saving the night clock style failed", "err", err)
		return
	}
	if d.nightStyle != nil {
		d.nightStyle.Set(nightStyleOptions[i])
	}
	d.wake()
}

// setAtNight saves the choice, shows it in Home Assistant, and lets the night take it up at once.
func (d *Display) setAtNight(i int) {
	if err := config.Set().Screen().AtNight(i >= 1, i == 2); err != nil {
		slog.Error("saving the night setting failed", "err", err)
		return
	}
	if d.atNight != nil {
		d.atNight.Set(atNightOptions[i])
	}
	if i == 0 {
		// Back to going dark: a glow still up would otherwise stay for the rest of the night.
		d.mu.Lock()
		glowing := d.nightGlow
		d.nightGlow = false
		d.mu.Unlock()
		if glowing {
			d.relight(true)
		}
	}
	d.wake()
}

// nightHoursChanged shows the hours chosen, on the screen or here, in Home Assistant.
func (d *Display) nightHoursChanged() {
	v := config.Get().Screen.Night
	if d.nightHours != nil {
		d.nightHours.Set(nightHoursLabel(v))
	}
	if d.nightStart != nil {
		from, to, ok := nightWindow(v)
		if !ok {
			from, to = nightOrDefault()
		}
		d.nightStart.Set(quarterText(from - from%15))
		d.nightEnd.Set(quarterText(to - to%15))
	}
}

// glowSteps is the night light's scale, 1 to 10, as backlight out of screen.BacklightMax: tight at the
// bottom, where one step is the difference between a glow and a light in a dark room.
var glowSteps = [...]int{1, 2, 3, 4, 6, 8, 10, 13, 16, 20}

// glowSetting is the night light's brightness on its 1 to 10 scale: the one set, or the panel's own.
// The Show 5 starts where the night light began; the Show 8's panel lights a dark room far more at the
// same backlight, so it starts lower.
func (d *Display) glowSetting() int {
	d.mu.Lock()
	wide := d.wide
	d.mu.Unlock()
	return glowFor(wide)
}

// glowFor is glowSetting for a panel already known, for callers holding mu.
func glowFor(wide bool) int {
	if v := config.Get().Screen.NightLightLevel; v > 0 {
		return v
	}
	if wide {
		return 3
	}
	return 6
}

// glowBacklight is the backlight the night light shows at on the panel. Wants mu, for wide.
func (d *Display) glowBacklight() int {
	return glowSteps[min(max(glowFor(d.wide), 1), len(glowSteps))-1]
}

func glowNumber(d *Display) *esphome.Number {
	n := &esphome.Number{
		Base: esphome.Base{
			ObjectID: "screen_night_light_level",
			Name:     "Night light brightness",
			Icon:     "mdi:lightbulb-night",
			Category: esphome.CategoryConfig,
		},
		Min: 1, Max: 10, Step: 1,
	}
	n.OnCommand = func(v float32) {
		if err := config.Set().Screen().NightLightLevel(int(v)); err != nil {
			slog.Error("saving the night light brightness failed", "err", err)
			return
		}
		n.Set(float32(d.glowSetting()))
		// Seen at once, so it can be set while looking at it in the dark.
		d.relight(true)
	}
	return n
}
