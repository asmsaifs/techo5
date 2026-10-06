//go:build !dot && !spot

package display

import (
	"image"
	"time"

	"github.com/HuskerMinion/techo5/echod/internal/config"
	"github.com/HuskerMinion/techo5/echod/internal/feature/dashboard"
	"github.com/HuskerMinion/techo5/echod/internal/hardware/touch"
)

// pickFor is how long the chooser stays up untouched: it is a question, and an unanswered one should
// not sit over the clock.
const pickFor = 5 * time.Second

// pagesOn is which of the two streamed pages a swipe in from the left could open: the dashboard when
// it is shown at all, the deck when a server is set for it.
func pagesOn() (dash, deck bool) {
	return dashboard.Get().Mode() != config.DashboardOff, dashboard.Get().DeckSet()
}

// openPage is the swipe in from the left on the clock. With both pages set up it asks which, in a
// narrow card; with one it opens that one at once, as it always did; with neither it does nothing.
func (d *Display) openPage() bool {
	dash, deck := pagesOn()
	switch {
	case dash && deck:
		d.mu.Lock()
		d.pickUntil = time.Now().Add(pickFor)
		d.mu.Unlock()
		d.wake()
		return true
	case deck:
		return d.openStreamDeck()
	case dash:
		return d.openDashboard()
	}
	return false
}

// pickUp is whether the chooser is on the screen.
func (d *Display) pickUp() bool {
	d.mu.Lock()
	defer d.mu.Unlock()
	return time.Now().Before(d.pickUntil)
}

// closePick puts the chooser away.
func (d *Display) closePick() {
	d.mu.Lock()
	d.pickUntil = time.Time{}
	d.mu.Unlock()
}

// pickGesture is a finger while the chooser is up. A tap on one of its buttons opens that page, a tap
// anywhere else puts the chooser away, and so does the swipe that brought it. It reports whether the
// gesture was its own: other gestures (the volume, the drawer) go on as they would without it.
func (d *Display) pickGesture(g touch.Gesture) bool {
	if d.r == nil {
		return false
	}
	switch {
	case g.Kind == touch.Tap:
		dash, deck := d.r.pickButtons()
		at := image.Pt(g.X, g.Y)
		d.closePick()
		switch {
		case at.In(dash):
			d.openDashboard()
		case at.In(deck):
			d.openStreamDeck()
		}
		d.wake()
		return true
	case g.Kind == touch.SwipeRight && g.X < d.r.drawerEdge():
		d.closePick()
		d.wake()
		return true
	}
	return false
}

// pickBox is the chooser's card: narrow, in the middle.
func (r *renderer) pickBox() image.Rectangle {
	w, h := r.s(300), r.s(170)
	return image.Rect((r.w-w)/2, (r.h-h)/2, (r.w-w)/2+w, (r.h-h)/2+h)
}

// pickButtons are the two buttons in the card, the dashboard above the deck.
func (r *renderer) pickButtons() (dash, deck image.Rectangle) {
	box := r.pickBox()
	in, gap, h := r.s(18), r.s(14), r.s(60)
	x0, x1 := box.Min.X+in, box.Max.X-in
	y := box.Min.Y + in
	dash = image.Rect(x0, y, x1, y+h)
	deck = image.Rect(x0, y+h+gap, x1, y+2*h+gap)
	return dash, deck
}

// pickCard draws the chooser over the clock.
func (r *renderer) pickCard(scene) {
	box := r.pickBox()
	r.roundShadow(box, r.cardRad(), float64(r.s(34)), r.s(12), shadowAlpha()*1.3)
	r.roundFill(box, r.cardRad(), surface(4), surface(2))
	r.roundHighlight(box, r.cardRad())
	dash, deck := r.pickButtons()
	for _, b := range []struct {
		at    image.Rectangle
		label string
	}{{dash, "Dashboard"}, {deck, "Deck"}} {
		r.bevel(b.at, shift(ember, 16), true)
		r.text(r.small, b.label, b.at.Min.X+(b.at.Dx()-r.width(r.small, b.label))/2, b.at.Min.Y+b.at.Dy()/2+r.s(8), cream)
	}
}
