//go:build !dot && !spot

package display

import (
	"image"
	"strings"

	"github.com/HuskerMinion/techo5/echod/internal/feature/notification"
)

// A phone's notification (feature/notification): a card the size of an event's pop-up, with the app
// and the phone over who it is from, and what it says when the text is to be shown.

// noteLines is as many lines as the card gives a notification's text.
const noteLines = 2

func (r *renderer) noteCard(n notification.Note) {
	box := r.popupBox()
	r.setNoteAt(box)
	r.roundShadow(box, r.cardRad(), float64(r.s(34)), r.s(12), shadowAlpha()*1.3)
	r.roundFill(box, r.cardRad(), surface(4), surface(2))
	r.roundHighlight(box, r.cardRad())

	in := r.rowIn()
	width := box.Dx() - 2*in
	heading := strings.ToUpper(n.App)
	if n.Phone != "" {
		heading += "  ·  " + strings.ToUpper(n.Phone)
	}
	hintW := r.width(r.tiny, dismissHint)
	r.rightText(r.tiny, dismissHint, box.Max.X-in, box.Min.Y+r.s(52), dim)
	r.text(r.tiny, clipText(r, r.tiny, heading, width-hintW-r.s(24)), box.Min.X+in, box.Min.Y+r.s(52), amber)

	title := n.Title
	if title == "" {
		title = n.Text // a notification with only words: they are its title
		n.Text = ""
	}
	if n.Text == "" {
		// Who it is from, alone: in the middle of what is left of the card.
		r.text(r.title, clipText(r, r.title, title, width), box.Min.X+in, box.Min.Y+r.s(166), cream)
		return
	}
	r.text(r.title, clipText(r, r.title, title, width), box.Min.X+in, box.Min.Y+r.s(118), cream)
	lines := r.wrap(r.small, n.Text, width)
	if len(lines) > noteLines {
		lines = append(lines[:noteLines-1], clipText(r, r.small, strings.Join(lines[noteLines-1:], " "), width))
	}
	y := box.Min.Y + r.s(172)
	for _, line := range lines {
		r.text(r.small, line, box.Min.X+in, y, dim)
		y += r.s(42)
	}
}

func (r *renderer) setNoteAt(b image.Rectangle) {
	r.weatherMu.Lock()
	r.noteAt = b
	r.weatherMu.Unlock()
}

// noteTapped is whether a tap at p landed on the notification's card as last drawn.
func (r *renderer) noteTapped(p image.Point) bool {
	r.weatherMu.Lock()
	defer r.weatherMu.Unlock()
	return !r.noteAt.Empty() && p.In(r.noteAt)
}
