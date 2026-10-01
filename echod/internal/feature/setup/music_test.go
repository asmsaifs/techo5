package setup

import (
	"net/url"
	"path/filepath"
	"testing"

	"github.com/HuskerMinion/techo5/echod/internal/config"
)

// The form as the page sends it carries the page's own token too, named "token": the Music Assistant
// token is its own field, and the page's is never taken for it.
func TestMusicAssistantSavesItsOwnToken(t *testing.T) {
	config.Use(filepath.Join(t.TempDir(), "state.json"))
	post := func(v url.Values) string {
		v.Set("token", "the-pages-own-token")
		v.Set("what", "music")
		return saveMusic(form(v))
	}
	if p := post(url.Values{"url": {"http://192.168.1.20:8095/"}, "matoken": {"ma-secret"}}); p != "" {
		t.Fatal(p)
	}
	if m := config.Get().MusicAssistant; m.URL != "http://192.168.1.20:8095" || m.Token != "ma-secret" {
		t.Errorf("saved %+v", m)
	}
	// Saved again with the token field left empty: the saved token stays.
	if p := post(url.Values{"url": {"http://192.168.1.20:8095"}}); p != "" {
		t.Fatal(p)
	}
	if got := config.Get().MusicAssistant.Token; got != "ma-secret" {
		t.Errorf("an empty field changed the token to %q", got)
	}
	if p := post(url.Values{"url": {"http://192.168.1.20:8095"}, "notoken": {"yes"}}); p != "" || config.Get().MusicAssistant.Token != "" {
		t.Errorf("remove left %q (%s)", config.Get().MusicAssistant.Token, p)
	}
	for _, bad := range []url.Values{
		{"url": {"http://user:pw@192.168.1.20:8095"}},
		{"url": {"ftp://192.168.1.20"}},
		{"url": {"http://192.168.1.20:8095"}, "matoken": {"two\twords"}},
	} {
		if post(bad) == "" {
			t.Errorf("%v was saved", bad)
		}
	}
}
