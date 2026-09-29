//go:build !dot && !spot

package display

import (
	"image"
	"testing"
	"time"

	"github.com/HuskerMinion/techo5/echod/internal/feature/remind"
	"golang.org/x/image/font"
)

// The card is checked by what it paints rather than by what it returns, because the only thing
// wrong with a card that draws the wrong code in the wrong place at the wrong size is a card that
// paints it. Every assertion here is about pixels: which of them changed, and where.

// changed is the box of every pixel two frames disagree on, and whether they disagree at all.
func changed(a, b *image.RGBA) (image.Rectangle, int) {
	box, n := image.Rectangle{}, 0
	for y := a.Rect.Min.Y; y < a.Rect.Max.Y; y++ {
		for x := a.Rect.Min.X; x < a.Rect.Max.X; x++ {
			if a.RGBAAt(x, y) != b.RGBAAt(x, y) {
				n++
				box = image.Rect(x, y, x+1, y+1).Union(box)
			}
		}
	}
	return box, n
}

// draw is a frame of the given size, drawn the way the device draws it.
func frame(s scene, w, h int) *image.RGBA {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	newRenderer(img).draw(s)
	return img
}

var activationAt = time.Date(2026, 9, 16, 14, 7, 0, 0, time.Local)

// The card changes the middle of the screen and nothing outside it: it is a card over the clock, so
// a device that has not been redeemed yet still has a clock, a touch screen and a volume.
func TestTheActivationCardIsACardOverTheClock(t *testing.T) {
	clock := scene{now: activationAt, phase: "idle"}
	coded := scene{now: activationAt, phase: "idle", activation: "839201"}

	img := frame(coded, showWide, showHigh)
	box, n := changed(frame(clock, showWide, showHigh), img)
	if n == 0 {
		t.Fatal("a code on the screen painted nothing at all")
	}
	r := newRenderer(img)

	// The shadow falls a little outside the card, and the clock is still behind it: what has to hold
	// is that the change is the card's own place, not that it stops at the card's exact edge. The
	// shadow is a blur a little wider than the card, which is what the slack here is.
	card := newRenderer(img).activationBox()
	slack := r.s(60)
	out := image.Rect(card.Min.X-slack, card.Min.Y-slack, card.Max.X+slack, card.Max.Y+slack)
	if !box.In(out) {
		t.Errorf("the card painted %v, which is not its own place %v", box, card)
	}
	if !card.In(box) {
		t.Errorf("the card painted %v, which does not cover the card's place %v", box, card)
	}
	// The card is a card and not a page: it leaves the clock visible round it on every side, and
	// its top is far enough down the panel that the date and time behind it are not covered.
	top := r.s(90) // the header lives in the top band (header.go)
	if card.Min.X <= 0 || card.Max.X >= showWide || card.Min.Y < top || card.Max.Y >= showHigh {
		t.Errorf("the card %v leaves no clock round it on a %dx%d panel", card, showWide, showHigh)
	}
	if !box.In(img.Rect) {
		t.Errorf("the card's shadow %v runs off the panel %v", box, img.Rect)
	}
}

// The code is the whole point of the card, so it is at the size the clock is set in, centred in the
// card, and wide enough to be read from the far side of a room and narrow enough not to run out of
// it. A code longer than the six digits a server sends is drawn smaller and whole rather than cut
// off, because a half-drawn code is a code that cannot be typed.
func TestTheCodeIsLargeCentredAndInsideTheCard(t *testing.T) {
	img := image.NewRGBA(image.Rect(0, 0, showWide, showHigh))
	r := newRenderer(img)
	box := r.activationBox()
	inner := box.Dx() - 2*r.rowIn()

	// Six digits is what the card is sized for, and it is drawn in the clock's own face: this is
	// the one thing on the screen somebody may be reading from across a room.
	if f := r.codeFace("839201", inner); f != r.big {
		t.Errorf("a six-digit code is drawn in a %v face, not the clock's own", f.Metrics().Height)
	}
	if h := r.big.Metrics().Height.Ceil(); h < 100 {
		t.Errorf("the code's face is %d tall; it wants to be readable across a room", h)
	}
	for _, code := range []string{"1", "839201", "1234567890123", "A1B2C3", "not a number at all"} {
		f := r.codeFace(code, inner)
		w := r.width(f, code)
		if w > inner {
			t.Errorf("the code %q is %d wide in the smallest face and the card's inside is %d", code, w, inner)
		}
		centre := box.Min.X + box.Dx()/2
		if x := centre - w/2; x < box.Min.X+r.rowIn() || x+w > box.Max.X-r.rowIn() {
			t.Errorf("the code %q runs from %d to %d, outside the card", code, x, x+w)
		}
	}
	// A code six digits long is a good deal wider than it is tall, which is what makes it read as a
	// code rather than as a clock: anything under a quarter of the card's width would look like one.
	if w := r.width(r.big, "839201"); w < box.Dx()/4 {
		t.Errorf("a six-digit code is %d wide in a card %d across; it does not read as a code", w, box.Dx())
	}
}

// Nothing on the card is clipped and nothing runs off it. A clipped sentence is the failure that
// looks fine in a preview and is unreadable on the device, and the four lines are stacked far
// enough down the card that the tallest of them still has room under it.
func TestNothingOnTheCardIsClipped(t *testing.T) {
	img := image.NewRGBA(image.Rect(0, 0, showWide, showHigh))
	r := newRenderer(img)
	box := r.activationBox()
	inner := box.Dx() - 2*r.rowIn()

	lines := []struct {
		face font.Face
		text string
	}{
		{r.tiny, "ACTIVATE THIS DEVICE"},
		{r.small, "Enter this code in the Xiaozhi console"},
		{r.tiny, activationWhere},
	}
	for _, l := range lines {
		if w := r.width(l.face, l.text); w > inner {
			t.Errorf("%q is %d wide and the card's inside is %d", l.text, w, inner)
		}
	}
	// The four lines are stacked in order and each starts below the one above it ends, so no two
	// of them can be sitting on the same pixels however the faces are set.
	bottoms := []struct {
		face font.Face
		top  int
	}{
		{r.tiny, activationHeadTop},
		{r.big, activationCodeTop},
		{r.small, activationWhatTop},
		{r.tiny, activationWhereTop},
	}
	for i, l := range bottoms {
		bottom := l.top - l.face.Metrics().Height.Ceil()
		if bottom <= 0 {
			t.Errorf("a line whose baseline is %d up the card starts above its own top edge", bottom)
		}
		if i == 0 {
			continue
		}
		if above := bottoms[i-1].top; l.top < above {
			t.Errorf("the line at %d is above the one at %d", l.top, above)
		}
	}
	// And the last of them is still a line's height above the card's bottom edge.
	if last := activationWhereTop + r.tiny.Metrics().Height.Ceil(); last > activationHBase {
		t.Errorf("the last line ends at %d and the card is %d tall", last, activationHBase)
	}
}

// A device that has been redeemed has nothing to say: the card is not the status page with the code
// left on it, it is gone the moment the code is, and the screen is the clock again.
func TestNoCodeMeansNoCard(t *testing.T) {
	for _, s := range []scene{
		{now: activationAt, phase: "idle"},
		{now: activationAt, phase: "idle", activation: ""},
		{now: activationAt, phase: "idle", activation: "   "},
	} {
		box, n := changed(frame(scene{now: activationAt, phase: "idle"}, showWide, showHigh), frame(s, showWide, showHigh))
		if n != 0 {
			t.Errorf("a scene with code %q painted %d pixels, from %v", s.activation, n, box)
		}
	}
}

// A code is the more urgent of the two things that want the middle of the screen. A reminder for
// this morning's milk is not going to wait for a session to open, and the code cannot be found
// again from a log once the screen has turned it over. The code does not sit on the reminder: the
// reminder's card is not drawn at all, so what is on the screen is the code and nothing under it.
func TestTheCodeGoesOverAReminder(t *testing.T) {
	reminder := scene{now: activationAt, phase: "idle", showReminder: true,
		reminder: remind.Reminder{Label: "Milk", At: activationAt.Add(2 * time.Minute)}}
	both := reminder
	both.activation = "839201"

	// With a code to redeem, the frame is the one with no reminder in it at all: not the code over
	// the top of the reminder, but the code instead of it.
	if _, n := changed(frame(both, showWide, showHigh), frame(scene{now: activationAt, phase: "idle", activation: "839201"}, showWide, showHigh)); n != 0 {
		t.Errorf("%d pixels of the reminder are showing around or through the code", n)
	}
	// The two are not the same picture, though: the reminder is genuinely there in the scene, and a
	// card that drew nothing whenever a reminder happened to be up would pass the check above.
	if _, n := changed(frame(both, showWide, showHigh), frame(reminder, showWide, showHigh)); n == 0 {
		t.Error("a code alongside a reminder drew neither")
	}
}
