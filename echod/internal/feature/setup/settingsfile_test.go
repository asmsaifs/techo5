package setup

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/HuskerMinion/techo5/echod/internal/config"
)

func TestSettingsDownloadLeavesSecretsOut(t *testing.T) {
	config.Use(filepath.Join(t.TempDir(), "state.json"))
	if err := config.Set().Home().HouseWord("lantern"); err != nil {
		t.Fatal(err)
	}
	f, c := letInPage(t)
	if w := get(f, "/setup/settings.json", nil); w.Code != http.StatusForbidden {
		t.Errorf("without being let in: %d", w.Code)
	}
	w := get(f, "/setup/settings.json", c)
	if w.Code != http.StatusOK {
		t.Fatalf("let in: %d", w.Code)
	}
	if !strings.Contains(w.Body.String(), `"format": "techo5-settings"`) {
		t.Errorf("not a settings file: %.200s", w.Body.String())
	}
	if strings.Contains(w.Body.String(), "lantern") {
		t.Error("the house word left the device in the settings file")
	}
}

func TestARestoreAsksThenApplies(t *testing.T) {
	config.Use(filepath.Join(t.TempDir(), "state.json"))
	restarted := 0
	was := restartAfterRestore
	restartAfterRestore = func() { restarted++ }
	t.Cleanup(func() { restartAfterRestore = was })

	f, c := letInPage(t)
	token := c.Value
	file := `{"format":"techo5-settings","version":1,"settings":{"screen":{"clock_24":true}}}`

	// Without the page's token, nothing is looked at.
	w := httptest.NewRecorder()
	f.serve(w, multipartPost("/setup/restore", [][2]string{{"token", "wrong"}, {"file", file}}, map[string]bool{"file": true}, "s.json", c))
	if w.Code != http.StatusForbidden {
		t.Errorf("another page's restore: %d", w.Code)
	}

	w = httptest.NewRecorder()
	f.serve(w, multipartPost("/setup/restore", [][2]string{{"token", token}, {"file", file}}, map[string]bool{"file": true}, "s.json", c))
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), "The screen") {
		t.Fatalf("looking at a file: %d %.300s", w.Code, w.Body.String())
	}
	if config.Get().Screen.Clock24 || restarted != 0 {
		t.Fatal("looking at a file changed the settings")
	}

	w = httptest.NewRecorder()
	f.serve(w, multipartPost("/setup/restore", [][2]string{{"token", token}, {"apply", "yes"}}, nil, "", c))
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), "Restored") {
		t.Fatalf("applying: %d %.300s", w.Code, w.Body.String())
	}
	if !config.Get().Screen.Clock24 || restarted != 1 {
		t.Errorf("after applying: clock24 %v, restarted %d", config.Get().Screen.Clock24, restarted)
	}

	// Applied once: a second apply has nothing to apply.
	w = httptest.NewRecorder()
	f.serve(w, multipartPost("/setup/restore", [][2]string{{"token", token}, {"apply", "yes"}}, nil, "", c))
	if w.Code != http.StatusSeeOther || restarted != 1 {
		t.Errorf("applying again: %d, restarted %d", w.Code, restarted)
	}
}

func TestARestoreRefusesWhatIsNotASettingsFile(t *testing.T) {
	config.Use(filepath.Join(t.TempDir(), "state.json"))
	f, c := letInPage(t)
	w := httptest.NewRecorder()
	f.serve(w, multipartPost("/setup/restore", [][2]string{{"token", c.Value}, {"file", "hello"}}, map[string]bool{"file": true}, "s.json", c))
	if w.Code != http.StatusSeeOther || !strings.Contains(w.Header().Get("Location"), "problem") {
		t.Errorf("a file that is not settings: %d %s", w.Code, w.Header().Get("Location"))
	}
}
