//go:build !dot && !spot

package display

import (
	"image/color"

	"github.com/HuskerMinion/techo5/echod/internal/feature/cast"
	"github.com/HuskerMinion/techo5/echod/internal/hardware/touch"
)

// The cast page: what a phone is sending, over the whole screen. It takes the screen when a cast
// starts and gives it back when the phone stops or a swipe in from the left edge ends it. It waits
// behind whatever is more pressing (a ring, a call, a turn, the settings), as the dashboard does; the
// cast carries on underneath and is there when the screen comes back to it.

// castScene decides whether the cast is the page.
func (d *Display) castScene(s *scene, sheetOrDrawer bool) {
	f := cast.Get()
	if d.r != nil {
		f.SetScreen(d.r.w, d.r.h)
	}
	want := f.Active() && s.phase == "idle" && !sheetOrDrawer &&
		!s.showCamera && !s.showWeather && !s.showRadar && !s.showCalendar && !s.showWifi && !s.bt.Pairing
	s.showCast = want
	d.mu.Lock()
	d.castShowing = want
	d.mu.Unlock()
}

// castGesture is a finger on the cast page. It has nothing to press: a swipe in from the left edge
// ends the cast, and the rest is ignored rather than passed on to a page under it.
func (d *Display) castGesture(g touch.Gesture) {
	if g.Kind == touch.SwipeRight && d.r != nil && g.X < d.r.drawerEdge() {
		cast.Get().Stop()
	}
}

// castPage draws the newest frame, or says the cast is starting.
func (r *renderer) castPage(s scene) {
	if cast.Get().Draw(r.dst) {
		return
	}
	r.fillRect(r.dst.Rect, color.RGBA{A: 255})
	msg := "Casting…"
	r.text(r.small, msg, (r.w-r.width(r.small, msg))/2, r.h/2, dim)
}
