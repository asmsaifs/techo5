//go:build !dot && !spot

package display

import (
	"image"
	"image/color"
	"strings"

	"golang.org/x/image/font"
)

// The activation card: a Xiaozhi device that has never been redeemed has a six-digit code, and
// until its owner types that code into Xiaozhi's console the device will not open a session with
// anything. The code is the only thing standing between a new unit and working, so it is said the
// only way that can be read from across a room - large, in the middle, with the place to type it
// named underneath.
//
// It is a card over the clock rather than a page of its own, on the same reasoning as the reminder
// and the pop-up: a card is something the screen is saying, not something it has become. The device
// behind it still has a clock, a touch screen and a speaker, and a person who is not redeeming the
// code right now should still be able to see what time it is and swipe the volume.

// The card's size, and where each line sits on it, in the Show 5's pixels: every length goes
// through paint.s, so the same layout comes out right on the Show 8's wider panel.
//
// The card is laid out from the top down, and the gaps are checked rather than eyeballed, so a face
// that grows cannot quietly walk a line off the bottom or onto the one above it. Its height is the
// reminder's, which puts its top at the same place: the header draws over every page, so a card
// that grew upward would have the date printed across its title.
const (
	activationWBase    = 660
	activationHBase    = 300 // the reminder's height, so the two cards sit at the same level
	activationHeadTop  = 44  // ACTIVATE THIS DEVICE
	activationCodeTop  = 170 // the digits
	activationWhatTop  = 224 // where to take the code
	activationWhereTop = 262 // and the address
)

// activationBox is the card's place in the middle of the screen.
func (r *renderer) activationBox() image.Rectangle {
	w, h := r.s(activationWBase), r.s(activationHBase)
	return image.Rect((r.w-w)/2, (r.h-h)/2, (r.w-w)/2+w, (r.h-h)/2+h)
}

// activationCard draws the code, and where to take it.
func (r *renderer) activationCard(s scene, code string) {
	box := r.activationBox()
	r.roundShadow(box, r.cardRad(), 30, 10, shadowAlpha()*1.3)
	r.roundFill(box, r.cardRad(), surface(4), surface(2))
	r.roundHighlight(box, r.cardRad())

	in := r.rowIn()
	inner := box.Dx() - 2*in
	// Everything is centred on the card's middle rather than started at its left edge, because the
	// card is read from the middle out: what somebody is looking for is the digits, and where they
	// end is not something the eye should have to be told.
	centre := box.Min.X + box.Dx()/2
	line := func(f font.Face, text string, top int, c color.Color) {
		r.text(f, text, centre-r.width(f, text)/2, box.Min.Y+r.s(top), c)
	}

	line(r.tiny, "ACTIVATE THIS DEVICE", activationHeadTop, amber)
	line(r.codeFace(code, inner), code, activationCodeTop, cream)
	line(r.small, "Enter this code in the Xiaozhi console", activationWhatTop, dim)
	line(r.tiny, activationWhere, activationWhereTop, dim)
}

// codeFace is the face the code is drawn in: the clock's own size, so it reads from across a room,
// stepping down only if a code is too wide to fit at that size. Six digits is what a server sends
// and what the face is chosen for; the steps are here so that a longer one from a self-hosted server
// is drawn small and complete rather than run off the edge of the card.
func (r *renderer) codeFace(code string, inner int) font.Face {
	for _, f := range []font.Face{r.big, r.title, r.body} {
		if r.width(f, code) <= inner {
			return f
		}
	}
	return r.micro
}

// activationWhere is the address the code is redeemed at, and the reason a code is written down at
// all: it is the project's own site rather than a redirect, so the sentence on the card is true
// whether or not anything in between is.
const activationWhere = "xiaozhi.me"

// code is the activation to show, and empty when there is none. It is trimmed here rather than
// where it is set, because a card is a page that can be drawn from a test as well as from the
// feature, and a page that draws an empty code is a page that has to be told not to.
func (s scene) code() string { return strings.TrimSpace(s.activation) }
