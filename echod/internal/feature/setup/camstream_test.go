package setup

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"testing"

	"github.com/HuskerMinion/techo5/echod/internal/config"
)

func postCamStream(user, pass string) string {
	r := httptest.NewRequest(http.MethodPost, "/setup/save", strings.NewReader(url.Values{"user": {user}, "pass": {pass}}.Encode()))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	return saves["camstream"](r)
}

// A password is set, kept when the field is left empty, and refused when short or more than one line.
func TestCamStreamLogin(t *testing.T) {
	config.Use(filepath.Join(t.TempDir(), "state.json"))
	if problem := postCamStream("recorder", "long enough here"); problem != "" {
		t.Fatal(problem)
	}
	if problem := postCamStream("recorder", ""); problem != "" {
		t.Fatal(problem)
	}
	if user, pass := config.Get().CamStream.Login(); user != "recorder" || pass != "long enough here" {
		t.Errorf("login %q:%q", user, pass)
	}
	for _, bad := range [][2]string{{"recorder", "short"}, {"recorder", "two\nlines!"}, {"a:b", "long enough here"}, {`a"b`, "long enough here"}} {
		if postCamStream(bad[0], bad[1]) == "" {
			t.Errorf("%q:%q was taken", bad[0], bad[1])
		}
	}
	if _, pass := config.Get().CamStream.Login(); pass != "long enough here" {
		t.Errorf("a refused save changed the password to %q", pass)
	}
}

// A device with no camera has no section for it.
func TestCamStreamSectionNeedsACamera(t *testing.T) {
	config.Use(filepath.Join(t.TempDir(), "state.json"))
	w := httptest.NewRecorder()
	camStreamSection(w, "token")
	if w.Body.Len() != 0 {
		t.Errorf("a camera streaming section with no camera: %s", w.Body.String())
	}
}
