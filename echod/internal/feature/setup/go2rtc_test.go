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

// The address is shown without its password, and saved back as shown, the password is kept.
func TestGo2RTCKeepsItsPassword(t *testing.T) {
	config.Use(filepath.Join(t.TempDir(), "state.json"))
	var user, pass string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		user, pass, _ = r.BasicAuth()
		_, _ = w.Write([]byte(`{"porch":{}}`))
	}))
	defer srv.Close()
	host := strings.TrimPrefix(srv.URL, "http://")

	post := func(address string) string {
		r := httptest.NewRequest(http.MethodPost, "/setup/save", strings.NewReader(url.Values{"address": {address}}.Encode()))
		r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		return saves["go2rtc"](r)
	}
	if problem := post("http://admin:s3cret@" + host); problem != "" {
		t.Fatal(problem)
	}
	if user != "admin" || pass != "s3cret" {
		t.Errorf("go2rtc was asked as %q:%q", user, pass)
	}

	w := httptest.NewRecorder()
	go2rtcSection(w, "token")
	if strings.Contains(w.Body.String(), "s3cret") {
		t.Error("the page shows go2rtc's password")
	}

	user, pass = "", ""
	if problem := post("http://" + host); problem != "" {
		t.Fatal(problem)
	}
	if user != "admin" || pass != "s3cret" || !strings.Contains(config.Get().Home.Go2RTC.Base, "admin:s3cret@") {
		t.Errorf("saved back as shown, the password was lost: %q:%q, %q", user, pass, config.Get().Home.Go2RTC.Base)
	}
}

// The kept login goes only to the same server the same way: not with another scheme or another port, and
// a refused address says nothing of it.
func TestGo2RTCKeepsItsPasswordForItsServerOnly(t *testing.T) {
	config.Use(filepath.Join(t.TempDir(), "state.json"))
	gone := httptest.NewServer(http.NotFoundHandler())
	gone.Close() // nothing answers there, so a refused or failed save is all that can happen
	host := strings.TrimPrefix(gone.URL, "http://")
	kept := "https://admin:s3cret@" + host
	if err := config.Set().Home().Go2RTC(config.Go2RTC{Base: kept}); err != nil {
		t.Fatal(err)
	}
	post := func(address string) string {
		r := httptest.NewRequest(http.MethodPost, "/setup/save", strings.NewReader(url.Values{"address": {address}}.Encode()))
		r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		return saves["go2rtc"](r)
	}
	for _, a := range []string{"ftp://" + host, "http://" + host, "https://" + strings.Split(host, ":")[0] + ":9"} {
		if problem := post(a); strings.Contains(problem, "s3cret") {
			t.Errorf("%s: the answer has the password: %s", a, problem)
		}
		if b := config.Get().Home.Go2RTC.Base; b != kept {
			t.Errorf("%s: the kept server changed to %q", a, b)
		}
	}
}
