//go:build !dot

package display

import (
	"context"
	"time"
)

// settleEvery is how often the backlight steps toward the room's level between readings. The light
// sensor reports a change, not every period: the input layer drops a value equal to the last, so a
// room that has stopped changing sends nothing. A backlight that moved only on a reading was left part
// of the way there until something else relit the screen.
const settleEvery = 500 * time.Millisecond

// settle keeps auto-brightness moving until ctx is canceled.
func (d *Display) settle(ctx context.Context) {
	t := time.NewTicker(settleEvery)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			d.settleStep()
		}
	}
}

// settleStep takes one smoothed step toward the room's level, if the backlight is not there yet.
//
// It also drives the light before an alarm, whose level is a matter of the time rather than of the
// room: nothing else relights while it rises, so without this the panel stayed where the sunrise
// started until the alarm. And once more as it ends, back to the room's level.
func (d *Display) settleStep() {
	rising := sunriseProgress(time.Now()) > 0
	d.mu.Lock()
	rising = rising && d.on
	ended := d.sunriseLit && !rising
	d.sunriseLit = rising
	due := d.autoOn && d.on && !d.settled
	d.mu.Unlock()
	switch {
	case rising || ended:
		d.relight(true)
	case due:
		d.relight(false)
	}
}
