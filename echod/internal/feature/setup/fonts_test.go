package setup

import (
	"bytes"
	"errors"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"testing"

	"github.com/HuskerMinion/techo5/echod/internal/config"
	"github.com/HuskerMinion/techo5/echod/internal/hardware/buttons"
)

// fontChoices is a screen offering three fonts and a fonts folder, recording what it is asked.
type fontChoices struct {
	font    int
	added   map[string]string
	removed []string
}

func (fc *fontChoices) screen() *ScreenChoices {
	return &ScreenChoices{
		Styles: []string{"Classic"}, Current: func() int { return 0 }, Choose: func(int) {},
		Fonts: []string{"Go", "Inter", "Comic Neue"}, FontNow: func() int { return fc.font }, ChooseFont: func(i int) { fc.font = i },
		OwnFonts: func() []OwnFont {
			return []OwnFont{{File: "a<b>.ttf", Size: 3 << 20, Name: "Noto Sans CJK KR"}, {File: "bad.otf", Problem: "it is not a font"}}
		},
		AddFont: func(name string, r io.Reader) error {
			b, err := io.ReadAll(r)
			if err != nil {
				return errors.New("the font did not arrive whole")
			}
			if string(b) == "junk" {
				return errors.New("it is not a font this device can read")
			}
			fc.added[name] = string(b)
			return nil
		},
		RemoveFont: func(file string) error {
			fc.removed = append(fc.removed, file)
			return nil
		},
		MaxFont: 1 << 20,
	}
}

// The screen's section saves the font through the display's own choice, and refuses one it does not
// have.
func TestTheScreenSectionSavesTheFont(t *testing.T) {
	config.Use(filepath.Join(t.TempDir(), "state.json"))
	fc := &fontChoices{added: map[string]string{}}
	SetScreen(fc.screen())
	t.Cleanup(func() { SetScreen(nil) })

	v := screenForm("0", "", false)
	v.Set("font", "2")
	if p := saveScreen(form(v)); p != "" || fc.font != 2 {
		t.Errorf("font %d (%s)", fc.font, p)
	}
	for _, bad := range []string{"3", "-1", "x", ""} {
		v.Set("font", bad)
		if p := saveScreen(form(v)); p == "" {
			t.Errorf("font %q was saved", bad)
		}
	}

	w := httptest.NewRecorder()
	screenSection(w, "tok")
	ownFontsSection(w, "tok")
	body := w.Body.String()
	for _, want := range []string{`<option value="2" selected>Comic Neue</option>`, "a&lt;b&gt;.ttf", "Noto Sans CJK KR", "not used: it is not a font", "/setup/font"} {
		if !strings.Contains(body, want) {
			t.Errorf("the page has no %q", want)
		}
	}
	if strings.Contains(body, "a<b>.ttf") {
		t.Error("a file's name was not escaped")
	}

	if p := removeFont(form(url.Values{"file": {"bad.otf"}})); p != "" || len(fc.removed) != 1 || fc.removed[0] != "bad.otf" {
		t.Errorf("removing: %v (%s)", fc.removed, p)
	}
}

// fontUpload is the page script's request: the token first, then the file.
func fontUpload(t *testing.T, token, name, body string, c *http.Cookie) *http.Request {
	t.Helper()
	var b bytes.Buffer
	mw := multipart.NewWriter(&b)
	if token != "" {
		_ = mw.WriteField("token", token)
	}
	part, _ := mw.CreateFormFile("font", name)
	_, _ = part.Write([]byte(body))
	_ = mw.Close()
	r := httptest.NewRequest(http.MethodPost, "/setup/font", &b)
	r.Header.Set("Content-Type", mw.FormDataContentType())
	if c != nil {
		r.AddCookie(c)
	}
	return r
}

// A font sent from the page is kept only from a browser let in, with the page's own token sent ahead
// of the file; what the device refuses is said back.
func TestAFontIsSentFromThePage(t *testing.T) {
	fc := &fontChoices{added: map[string]string{}}
	SetScreen(fc.screen())
	t.Cleanup(func() { SetScreen(nil) })
	f := build()
	f.Open()
	c := ask(t, f)
	f.button(buttons.Event{Name: buttons.Action, Kind: buttons.Tap})

	send := func(r *http.Request) *httptest.ResponseRecorder {
		w := httptest.NewRecorder()
		f.serve(w, r)
		return w
	}
	if w := send(fontUpload(t, c.Value, "kr.otf", "font bytes", nil)); w.Code != http.StatusForbidden {
		t.Errorf("without the cookie: %d", w.Code)
	}
	if w := send(fontUpload(t, "wrong", "kr.otf", "font bytes", c)); w.Code != http.StatusForbidden {
		t.Errorf("with another token: %d", w.Code)
	}
	if w := send(fontUpload(t, "", "kr.otf", "font bytes", c)); w.Code != http.StatusForbidden {
		t.Errorf("with no token: %d", w.Code)
	}
	if len(fc.added) != 0 {
		t.Fatalf("kept %v from a request that was refused", fc.added)
	}
	if w := send(fontUpload(t, c.Value, "kr.otf", "junk", c)); w.Code != http.StatusBadRequest || !strings.Contains(w.Body.String(), "not a font") {
		t.Errorf("a bad font: %d %q", w.Code, w.Body.String())
	}
	if w := send(fontUpload(t, c.Value, "big.otf", strings.Repeat("x", 2<<20), c)); w.Code != http.StatusBadRequest || len(fc.added) != 0 {
		t.Errorf("a font over the limit: %d, kept %d", w.Code, len(fc.added))
	}
	if w := send(fontUpload(t, c.Value, "kr.otf", "font bytes", c)); w.Code != http.StatusNoContent || fc.added["kr.otf"] != "font bytes" {
		t.Errorf("a good font: %d, kept %v", w.Code, fc.added)
	}

	SetScreen(nil)
	if w := send(fontUpload(t, c.Value, "kr.otf", "font bytes", c)); w.Code != http.StatusNotFound {
		t.Errorf("a device with no screen took a font: %d", w.Code)
	}
}
