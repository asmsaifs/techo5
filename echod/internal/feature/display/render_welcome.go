//go:build !dot && !spot

package display

import (
	"image"
	"strings"

	"github.com/HuskerMinion/techo5/echod/internal/config"
)

// welcome is the first-run card: what to say, where the settings are, and that a tap begins.
// It shows until it is tapped once, then never again.
func (r *renderer) welcome(s scene) {
	r.useFont()
	name := config.Get().Device.Name
	if name == "" {
		name = "TECHO5"
	}
	r.bevel(image.Rect(r.margin, 40, r.w-r.margin, r.h-40), shift(walnut, 10), true)
	r.text(r.title, "Hello", r.margin+40, 118, amber)
	word := strings.ReplaceAll(config.Get().Wake.Slot(0).ID, "_", " ")
	if word == "" {
		word = "the wake word"
	} else {
		word = strings.ToUpper(word[:1]) + word[1:]
	}
	lines := []string{
		"This is " + name + ". Say \"" + word + "\" and ask for anything.",
		"Swipe down from the top for settings: Wi-Fi, Bluetooth, cameras, radio, themes.",
		"Say \"show the front door\" or \"go home\" to move the screen.",
	}
	y := 190
	for _, para := range lines {
		for _, line := range r.wrap(r.small, para, r.w-2*r.margin-80) {
			r.text(r.small, line, r.margin+40, y, cream)
			y += 44
		}
	}
	hint := "Tap anywhere to begin"
	r.text(r.body, hint, (r.w-r.width(r.body, hint))/2, r.h-78, amber)
}
