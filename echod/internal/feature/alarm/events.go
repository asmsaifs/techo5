package alarm

import (
	"strings"
	"time"

	"github.com/HuskerMinion/techo5/echod/internal/component"
	"github.com/HuskerMinion/techo5/echod/internal/config"
)

// Event is fired on Home Assistant's bus at each step of an alarm, for an automation to start the day
// with: the radio, the blinds, a light. Its event field is
//
//   - ringing: an alarm went off and is ringing
//   - silent: a silent alarm went off; it makes no sound and has nothing to stop, so this is all of it
//   - snoozed: a ringing alarm was put off; it rings again later, with another ringing
//   - stopped: a ringing alarm was stopped, by a press, by voice, from Home Assistant, or by running
//     its course
//
// with the alarm's id and label, the time it was due, and the device it went off on: every device's
// events arrive on the one bus under one name. Reminders are not alarms and fire nothing here.
const Event = "esphome.techo5_alarm"

// fireEvent puts one step of an alarm on the bus. key is the alarm's, with a snooze's prefix taken off
// so that an alarm and its snoozes share an id.
func fireEvent(event, key, label string, due time.Time) {
	component.Fire.Emit(component.Event{Name: Event, Data: map[string]string{
		"event":  event,
		"id":     strings.TrimPrefix(key, "snooze:"),
		"label":  label,
		"due":    due.Format(time.RFC3339),
		"device": config.Get().Device.Name,
	}})
}

// setFromHA is SetOn with the alarm made silent or not, as the action asking for it says: setting the
// same alarm again with the other action changes which kind it is.
func (a *Alarms) setFromHA(hour, minute int, days uint8, label, date string, silent bool) (config.Alarm, error) {
	return a.setOn(hour, minute, days, label, date, &silent)
}
