package setup

import (
	"fmt"
	"html"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"sync/atomic"

	"github.com/HuskerMinion/techo5/echod/internal/config"
	"github.com/HuskerMinion/techo5/echod/internal/feature/home"
)

// The screen's own settings that a device far from its owner still needs set from a phone: how the
// clock looks, the slideshow, and weather art in place of its photos. On the Screen & Photos tab, over
// the photos; the rest of what the screen shows is still set on the screen, where it can be seen.

// ScreenChoices is the clock's look as the screen offers it, handed in by the display, which imports
// this package rather than the other way round. Choose saves one and tells Home Assistant.
type ScreenChoices struct {
	Styles  []string
	Current func() int
	Choose  func(int)
}

var screen atomic.Pointer[ScreenChoices]

// SetScreen is how the display offers its choices; a device with no screen never calls it.
func SetScreen(s *ScreenChoices) { screen.Store(s) }

// slideshowModes are the slideshow's modes as the page names them, with what the config keeps.
var slideshowModes = []struct{ label, value string }{
	{"Off", ""},
	{"Behind the clock", config.SlideshowBackground},
	{"Screensaver, after a while", config.SlideshowScreensaver},
}

func screenSection(w http.ResponseWriter, token string) {
	s := screen.Load()
	if s == nil {
		return
	}
	fmt.Fprint(w, `<fieldset><legend>The screen</legend><form method="post" action="/setup/save">`)
	hidden(w, token, "screen", "photos")
	fmt.Fprint(w, `<label for="clockstyle">Clock style</label><select id="clockstyle" name="clockstyle">`)
	cur := s.Current()
	for i, l := range s.Styles {
		fmt.Fprintf(w, `<option value="%d"%s>%s</option>`, i, selected(i == cur), html.EscapeString(l))
	}
	fmt.Fprint(w, `</select><label for="slideshow">Slideshow</label><select id="slideshow" name="slideshow">`)
	mode := home.Get().SlideshowMode()
	for _, m := range slideshowModes {
		fmt.Fprintf(w, `<option value="%s"%s>%s</option>`, m.value, selected(m.value == mode), m.label)
	}
	art := ""
	if home.Get().SlideshowArt() {
		art = " checked"
	}
	fmt.Fprintf(w, `</select>
	 <p><label><input type="checkbox" name="art" value="yes" style="width:auto"%s> Weather art in place of photos</label></p>
	 <p class="note">Weather art is a landscape the device draws for the weather and the time of day, with rain,
	  snow and clouds moving over it. It needs no photos and no Home Assistant.</p>
	 <p><button type="submit">Save</button></p></form></fieldset>`, art)
}

func saveScreen(r *http.Request) string {
	s := screen.Load()
	if s == nil {
		return "this device has no screen"
	}
	i, err := strconv.Atoi(r.PostFormValue("clockstyle"))
	if err != nil || i < 0 || i >= len(s.Styles) {
		return "that is not a clock style this device has"
	}
	mode := r.PostFormValue("slideshow")
	known := false
	for _, m := range slideshowModes {
		known = known || m.value == mode
	}
	if !known {
		return "that is not a slideshow this device has"
	}
	if i != s.Current() {
		s.Choose(i)
	}
	h := home.Get()
	h.ChooseSlideshowMode(mode)
	// The mode first, then the art: ticking weather art with the slideshow off turns it on behind the
	// clock (SetSlideshowArt), and choosing Off with the art already ticked turns the slideshow off.
	if on := r.PostFormValue("art") == "yes"; on != h.SlideshowArt() {
		h.SetSlideshowArt(on)
	}
	slog.Info("setup page: screen set", "clock style", strings.ToLower(s.Styles[i]), "slideshow", mode, "art", h.SlideshowArt())
	return ""
}
