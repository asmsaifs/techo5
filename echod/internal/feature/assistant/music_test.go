package assistant

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/HuskerMinion/techo5/echod/internal/config"
	"github.com/HuskerMinion/techo5/echod/internal/lib/musicassistant"
)

func item(uri, name, kind string) musicassistant.Item {
	return musicassistant.Item{URI: uri, Name: name, MediaType: kind}
}

// What was said by name wins, artists first; then the kind asked for; then an artist over a track.
func TestChooseMusic(t *testing.T) {
	r := musicassistant.Results{
		Artists: []musicassistant.Item{item("a:1", "Eagles", "artist")},
		Tracks:  []musicassistant.Item{item("t:1", "Hotel California", "track"), item("t:2", "Eagles Fly", "track")},
		Albums:  []musicassistant.Item{item("b:1", "Hotel California", "album")},
	}
	for _, c := range []struct{ query, kind, want string }{
		{"eagles", "", "a:1"},
		{"Hotel California", "", "t:1"},      // a track and an album share the name: the track, before albums
		{"Hotel California", "album", "b:1"}, // unless the album was asked for
		{"something else", "", "a:1"},        // nothing by name: the first artist
		{"something else", "track", "t:1"},
	} {
		got, ok := chooseMusic(r, c.query, c.kind)
		if !ok || got.URI != c.want {
			t.Errorf("%q as %q chose %q, want %q", c.query, c.kind, got.URI, c.want)
		}
	}
	if _, ok := chooseMusic(musicassistant.Results{}, "x", ""); ok {
		t.Error("nothing found chose something")
	}
}

// The service searched first: the one named aloud when the server has it, else the setting's, else
// none; and a server that will not list its services still gets the common names and the setting.
func TestMusicSource(t *testing.T) {
	config.Use(filepath.Join(t.TempDir(), "config.json"))
	forbid := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if forbid {
			w.WriteHeader(http.StatusForbidden)
			return
		}
		w.Write([]byte(`[{"instance_id":"ytmusic--a1","domain":"ytmusic","name":"YouTube Music","available":true},
			{"instance_id":"spotify--b2","domain":"spotify","name":"Spotify","available":true}]`))
	}))
	defer srv.Close()
	c := musicassistant.Client{URL: srv.URL, Token: "tok"}
	ctx := context.Background()

	check := func(label, said, wantID, wantUnknown string) {
		t.Helper()
		from, unknown := musicSource(ctx, c, said)
		if from.InstanceID != wantID || unknown != wantUnknown {
			t.Errorf("%s: %q gave %q (unknown %q), want %q (unknown %q)", label, said, from.InstanceID, unknown, wantID, wantUnknown)
		}
	}
	check("nothing named, no setting", "", "", "")
	check("named", "YouTube Music", "ytmusic--a1", "")
	check("named, not on this server", "Tidal", "", "Tidal")

	if err := config.Set().MusicAssistant().SetSource("spotify--b2"); err != nil {
		t.Fatal(err)
	}
	check("the setting", "", "spotify--b2", "")
	check("named over the setting", "youtube music", "ytmusic--a1", "")
	check("an unknown name keeps the setting", "the kitchen", "spotify--b2", "the kitchen")
	check("any service at all", "Any", "spotify--b2", "")

	forbid = true
	check("not listed: the setting as it is", "", "spotify--b2", "")
	check("not listed: a common name", "YouTube Music", "ytmusic", "")
	check("not listed: an unknown name is not called unknown", "my mixtapes", "spotify--b2", "")
}

// fakeMA is a Music Assistant answering the services list and searches: scoped is what a search of
// one service returns, all what a search of everything returns; empty for the server failing it.
func fakeMA(t *testing.T, scoped, all string) musicassistant.Client {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Command string         `json:"command"`
			Args    map[string]any `json:"args"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Error(err)
		}
		switch {
		case req.Command == "providers":
			w.Write([]byte(`[{"instance_id":"ytmusic--a1","domain":"ytmusic","name":"YouTube Music","available":true}]`))
		case req.Args["providers"] != nil && scoped == "":
			w.WriteHeader(http.StatusInternalServerError)
		case req.Args["providers"] != nil:
			w.Write([]byte(scoped))
		case all == "":
			w.WriteHeader(http.StatusInternalServerError)
		default:
			w.Write([]byte(all))
		}
	}))
	t.Cleanup(srv.Close)
	return musicassistant.Client{URL: srv.URL, Token: "tok"}
}

// What is played, and what is said about where from, with YouTube Music as the Music source.
func TestFindMusic(t *testing.T) {
	config.Use(filepath.Join(t.TempDir(), "config.json"))
	if err := config.Set().MusicAssistant().SetSource("ytmusic--a1"); err != nil {
		t.Fatal(err)
	}
	const (
		ytExact = `{"provider":"ytmusic--a1","uri":"ytmusic--a1://track/1","name":"Our Song","media_type":"track"}`
		ytNear  = `{"provider":"ytmusic--a1","uri":"ytmusic--a1://track/2","name":"Our Song (cover)","media_type":"track"}`
		libOurs = `{"provider":"library","uri":"library://track/9","name":"Our Song","media_type":"track"}`
		spotify = `{"provider":"spotify--b2","uri":"spotify--b2://track/3","name":"Our Song","media_type":"track"}`
	)
	tracks := func(items ...string) string { return `{"tracks":[` + strings.Join(items, ",") + `]}` }
	ctx := context.Background()
	for _, tc := range []struct {
		label, scoped, all, want, said string
	}{
		{"the service has it by name", tracks(ytExact), tracks(libOurs), "ytmusic--a1://track/1", "from YouTube Music"},
		{"the library's by name beats the service's near miss", tracks(ytNear), tracks(ytNear, libOurs), "library://track/9", "nothing by that name on YouTube Music"},
		{"no one has it by name: the service's nearest", tracks(ytNear), tracks(`{"provider":"library","uri":"library://track/8","name":"Our Songs","media_type":"track"}`), "ytmusic--a1://track/2", "from YouTube Music"},
		{"the service's search fails: the library", "", tracks(libOurs), "library://track/9", "could not be searched just now"},
		{"an old server searches everything when asked for one", tracks(spotify), tracks(spotify), "spotify--b2://track/3", "nothing by that name on YouTube Music"},
		{"a library item mapped to the service is the service's", tracks(`{"provider":"library","uri":"library://track/7","name":"Our Song","media_type":"track","provider_mappings":[{"provider_instance":"ytmusic--a1","provider_domain":"ytmusic"}]}`), tracks(libOurs), "library://track/7", "from YouTube Music"},
	} {
		f, err := findMusic(ctx, fakeMA(t, tc.scoped, tc.all), "Our Song", "", "")
		if err != nil {
			t.Errorf("%s: %v", tc.label, err)
			continue
		}
		if f.pick.URI != tc.want || !strings.Contains(f.said(), tc.said) {
			t.Errorf("%s: played %q, said %q; want %q, saying %q", tc.label, f.pick.URI, f.said(), tc.want, tc.said)
		}
	}
	// The whole library's search failing still plays the service's nearest.
	if f, err := findMusic(ctx, fakeMA(t, tracks(ytNear), ""), "Our Song", "", ""); err != nil || f.pick.URI != "ytmusic--a1://track/2" {
		t.Errorf("the library failing: played %q (%v)", f.pick.URI, err)
	}
	if _, err := findMusic(ctx, fakeMA(t, tracks(), tracks()), "Our Song", "", ""); err == nil {
		t.Error("nothing anywhere was not an error")
	}
	// A token that may not list the services: "YouTube Music" is taken by its domain, and the
	// service's own items, held under an instance id, still count as its.
	forbidden := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Command string `json:"command"`
		}
		json.NewDecoder(r.Body).Decode(&req)
		if req.Command == "providers" {
			w.WriteHeader(http.StatusForbidden)
			return
		}
		w.Write([]byte(tracks(ytExact)))
	}))
	defer forbidden.Close()
	f, err := findMusic(ctx, musicassistant.Client{URL: forbidden.URL, Token: "tok"}, "Our Song", "", "YouTube Music")
	if err != nil || f.pick.URI != "ytmusic--a1://track/1" || !strings.Contains(f.said(), "from YouTube Music") {
		t.Errorf("services not listed: played %q, said %q (%v)", f.pick.URI, f.said(), err)
	}

	f, err = findMusic(ctx, fakeMA(t, tracks(ytExact), tracks()), "Our Song", "", "the kitchen")
	if err != nil || !strings.Contains(f.said(), `"the kitchen" is not one of`) || !strings.Contains(f.said(), "from YouTube Music") {
		t.Errorf("an unknown service named: said %q (%v)", f.said(), err)
	}
}
