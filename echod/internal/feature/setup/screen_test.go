package setup

import (
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"testing"

	"github.com/HuskerMinion/techo5/echod/internal/config"
	"github.com/HuskerMinion/techo5/echod/internal/feature/home"
)

func screenForm(style, slideshow string, art bool) url.Values {
	v := url.Values{"clockstyle": {style}, "slideshow": {slideshow}}
	if art {
		v.Set("art", "yes")
	}
	return v
}

// The screen's section saves the clock style through the display's own choice (which tells Home
// Assistant), the slideshow's mode, and weather art; what the device does not have is refused.
func TestTheScreenSectionSaves(t *testing.T) {
	config.Use(filepath.Join(t.TempDir(), "state.json"))
	chosen := 0
	SetScreen(&ScreenChoices{Styles: []string{"Classic", "Big", "Words"}, Current: func() int { return chosen }, Choose: func(i int) { chosen = i }})
	t.Cleanup(func() { SetScreen(nil) })

	if p := saveScreen(form(screenForm("2", config.SlideshowBackground, true))); p != "" {
		t.Fatal(p)
	}
	if chosen != 2 || home.Get().SlideshowMode() != config.SlideshowBackground || !home.Get().SlideshowArt() {
		t.Errorf("saved style %d, slideshow %q, art %v", chosen, home.Get().SlideshowMode(), home.Get().SlideshowArt())
	}
	if p := saveScreen(form(screenForm("2", "", true))); p != "" || home.Get().SlideshowMode() != "" {
		t.Errorf("choosing Off with the art ticked left the slideshow %q (%s)", home.Get().SlideshowMode(), p)
	}
	for _, bad := range []url.Values{screenForm("9", "", false), screenForm("x", "", false), screenForm("0", "sideways", false)} {
		if p := saveScreen(form(bad)); p == "" {
			t.Errorf("%v was saved", bad)
		}
	}
}

// A device without a screen shows no such section, and refuses a save of it.
func TestNoScreenNoSection(t *testing.T) {
	SetScreen(nil)
	w := httptest.NewRecorder()
	screenSection(w, "tok")
	if strings.Contains(w.Body.String(), "Clock style") {
		t.Error("a device with no screen offered a clock style")
	}
	if saveScreen(form(screenForm("0", "", false))) == "" {
		t.Error("a device with no screen saved one")
	}
}
