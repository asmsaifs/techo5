//go:build !dot && !spot

package display

import (
	"image"
	"image/color"

	"golang.org/x/image/font"

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
	want := (f.Active() || f.Asking() != "") && s.phase == "idle" && !sheetOrDrawer &&
		!s.showCamera && !s.showWeather && !s.showRadar && !s.showCalendar && !s.showWifi && !s.bt.Pairing
	s.showCast = want
	d.mu.Lock()
	d.castShowing = want
	d.mu.Unlock()
}

// castGesture is a finger on the cast page. It has nothing to press: a swipe in from the left edge
// ends the cast, and the rest is ignored rather than passed on to a page under it.
func (d *Display) castGesture(g touch.Gesture) {
	// A phone waiting to be accepted: the left half of the buttons sends it away, the right half lets
	// it in. Nothing else on this page answers.
	if cast.Get().Asking() != "" {
		if g.Kind == touch.Tap && d.r != nil && d.r.actionDecided(g.Y) {
			cast.Get().Decide(g.X >= d.r.w/2)
		}
		return
	}
	if g.Kind == touch.SwipeRight && d.r != nil && g.X < d.r.drawerEdge() {
		cast.Get().Stop()
	}
}

// castPage draws the newest frame, or says the cast is starting.
func (r *renderer) castPage(s scene) {
	if who := cast.Get().Asking(); who != "" {
		r.castAsk(who)
		return
	}
	if cast.Get().Draw(r.dst) {
		r.castTitle(cast.Get().Title())
		return
	}
	r.fillRect(r.dst.Rect, color.RGBA{A: 255})
	msg := "Casting…"
	r.text(r.small, msg, (r.w-r.width(r.small, msg))/2, r.h/2, dim)
}

// castAsk is the question a phone's cast waits behind: who, and Decline or Accept where every page's
// answers are. The call's colors, for the same reason: a wrong tap here shows the room a stranger's
// screen.
func (r *renderer) castAsk(who string) {
	r.fillRect(r.dst.Rect, color.RGBA{A: 255})
	title := "Cast to this screen?"
	r.text(r.title, title, (r.w-r.width(r.title, title))/2, r.s(80), amber)
	face := r.body
	for _, f := range []font.Face{r.big, r.title} {
		if r.width(f, who) <= r.w-2*r.margin {
			face = f
			break
		}
	}
	r.text(face, who, (r.w-r.width(face, who))/2, r.s(220), cream)
	note := "wants to show its screen here"
	r.text(r.small, note, (r.w-r.width(r.small, note))/2, r.s(270), dim)

	decline, accept := r.actionHalves()
	rad := float64(r.s(actionRadius))
	mid := (decline.Min.Y + decline.Max.Y) / 2
	r.roundButton(decline, rad, declineRed)
	r.roundButton(accept, rad, answerGreen)
	r.text(r.title, "Decline", decline.Min.X+(decline.Dx()-r.width(r.title, "Decline"))/2, mid+r.s(16), color.White)
	r.text(r.title, "Accept", accept.Min.X+(accept.Dx()-r.width(r.title, "Accept"))/2, mid+r.s(16), color.White)
}

// castTitle says what is playing, on a dark band over the top of the picture, for the first moments.
func (r *renderer) castTitle(title string) {
	if title == "" {
		return
	}
	line := clipText(r, r.small, "Casting: "+title, r.w-2*r.margin-r.s(32))
	w := r.width(r.small, line) + r.s(32)
	box := image.Rect((r.w-w)/2, r.s(14), (r.w+w)/2, r.s(14)+r.s(52))
	r.roundButton(box, float64(r.s(26)), color.RGBA{A: 185})
	r.text(r.small, line, box.Min.X+r.s(16), box.Min.Y+r.s(36), cream)
}
