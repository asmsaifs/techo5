//go:build !dot

package setup

import (
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"testing"

	"github.com/HuskerMinion/techo5/echod/internal/config"
)

// The Talk through cameras form: an address per camera on the list, kept only for cameras on it and
// only as rtsp:// with no login in it, and a password that is written but never shown, and kept when
// its field is left empty.
func TestTalkBackForm(t *testing.T) {
	config.Use(filepath.Join(t.TempDir(), "state.json"))
	if err := config.Set().Home().Cameras([]config.Camera{{Entity: "camera.front_door", Name: "Front <door>"}, {Entity: "camera.deck", Name: "Deck"}}); err != nil {
		t.Fatal(err)
	}
	post := func(v url.Values) string {
		v.Set("token", "the-pages-own-token")
		v.Set("what", "talkback")
		return saveTalkBack(form(v))
	}
	ok := url.Values{"user": {"admin"}, "pass": {"cam-secret"},
		"entity": {"camera.front_door", "camera.deck", "camera.not_on_the_list"},
		"addr":   {"rtsp://192.168.1.40:554/h264Preview_01_main", "", ""}}
	if p := post(ok); p != "" {
		t.Fatal(p)
	}
	c := config.Get().TalkBack
	if c.User != "admin" || c.Pass != "cam-secret" || len(c.Cameras) != 1 || c.Cameras["camera.front_door"] != "rtsp://192.168.1.40:554/h264Preview_01_main" {
		t.Errorf("saved %+v", c)
	}
	// Again with the password left empty: the saved one stays.
	ok.Del("pass")
	if p := post(ok); p != "" || config.Get().TalkBack.Pass != "cam-secret" {
		t.Errorf("an empty field changed the password (%s)", p)
	}
	for _, bad := range []url.Values{
		{"entity": {"camera.deck"}, "addr": {"rtsp://admin:pw@192.168.1.40/x"}},
		{"entity": {"camera.deck"}, "addr": {"http://192.168.1.40/x"}},
		{"entity": {"camera.deck"}, "addr": {"rtsp://192.168.1.40/x y"}},
		{"entity": {"camera.deck", "camera.front_door"}, "addr": {"rtsp://192.168.1.40/x"}},
		{"user": {"admin\r\nX: y"}},
		{"user": {`ad"min`}},
		{"user": {strings.Repeat("a", 129)}},
		// A camera not on the list: Home Assistant's list is not to hand, and the address is not
		// dropped quietly.
		{"entity": {"camera.not_on_the_list"}, "addr": {"rtsp://192.168.1.41/x"}},
	} {
		if post(bad) == "" {
			t.Errorf("%v was saved", bad)
		}
	}

	// The page: the camera's name escaped, the address shown, the password never.
	rec := httptest.NewRecorder()
	talkBackSection(rec, "tok")
	page := rec.Body.String()
	for _, want := range []string{`autocomplete="new-password"`, "Front &lt;door&gt;", "rtsp://192.168.1.40:554/h264Preview_01_main", "set; leave empty to keep it", "The switch is off"} {
		if !strings.Contains(page, want) {
			t.Errorf("the section does not say %q", want)
		}
	}
	if strings.Contains(page, "cam-secret") || strings.Contains(page, "<door>") {
		t.Error("the section shows the password, or a name unescaped")
	}
}
