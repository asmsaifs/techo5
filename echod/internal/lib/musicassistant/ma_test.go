package musicassistant

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// The commands go as Music Assistant takes them: POST /api, the token as a bearer, the command and its
// args in the body; and its search results come back by kind.
func TestSearchAndPlay(t *testing.T) {
	var seen []map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api" || r.Method != http.MethodPost {
			t.Errorf("%s %s, want POST /api", r.Method, r.URL.Path)
		}
		if r.Header.Get("Authorization") != "Bearer tok" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		var body map[string]any
		json.NewDecoder(r.Body).Decode(&body)
		seen = append(seen, body)
		if body["command"] == "music/search" {
			w.Write([]byte(`{"artists":[{"uri":"library://artist/1","name":"Eagles","media_type":"artist"}],"tracks":[{"uri":"spotify://track/x","name":"Take It Easy","media_type":"track","artists":[{"name":"Eagles"}]}]}`))
			return
		}
		w.Write([]byte(`null`))
	}))
	defer srv.Close()

	c := Client{URL: srv.URL + "/", Token: "tok"}
	r, err := c.Search(context.Background(), "eagles", []string{"artist", "track"}, 3)
	if err != nil {
		t.Fatal(err)
	}
	if len(r.Artists) != 1 || r.Artists[0].URI != "library://artist/1" || r.Tracks[0].By() != "Eagles" {
		t.Errorf("search came back as %+v", r)
	}
	if err := c.Play(context.Background(), "aa:bb", "library://artist/1"); err != nil {
		t.Fatal(err)
	}
	args := seen[1]["args"].(map[string]any)
	if seen[1]["command"] != "player_queues/play_media" || args["queue_id"] != "aa:bb" || args["media"] != "library://artist/1" || args["option"] != "replace" {
		t.Errorf("play sent %+v", seen[1])
	}

	bad := Client{URL: srv.URL, Token: "wrong"}
	if _, err := bad.Search(context.Background(), "x", nil, 1); err == nil || err.Error() != "music assistant refused the token" {
		t.Errorf("a wrong token gave %v", err)
	}
}

// The device's player is found by its own id among each player's protocols, in the players the
// token may use; one it may not use is said as that, with what to do about it.
func TestPlayer(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`[{"player_id":"media_player.desk","available":true,"playback_state":"playing","output_protocols":[{"output_protocol_id":"native"},{"output_protocol_id":"aa:bb:cc:dd:ee:ff"}]}]`))
	}))
	defer srv.Close()
	c := Client{URL: srv.URL, Token: "tok"}
	p, err := c.Player(context.Background(), "AA:BB:CC:DD:EE:FF")
	if err != nil || p.ID != "media_player.desk" || !p.Available || p.State != "playing" {
		t.Errorf("%+v %v", p, err)
	}
	if _, err := c.Player(context.Background(), "11:22:33:44:55:66"); err == nil || !strings.Contains(err.Error(), "allowed players") {
		t.Errorf("a device the token may not use: %v", err)
	}
}

// A search can be held to services, which go to Music Assistant as its providers argument; without
// any, the argument is left out and the whole library is searched.
func TestSearchProviders(t *testing.T) {
	var args []map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		json.NewDecoder(r.Body).Decode(&body)
		args = append(args, body["args"].(map[string]any))
		w.Write([]byte(`{}`))
	}))
	defer srv.Close()
	c := Client{URL: srv.URL, Token: "tok"}
	if _, err := c.Search(context.Background(), "eagles", nil, 3, "ytmusic--a1"); err != nil {
		t.Fatal(err)
	}
	if _, err := c.Search(context.Background(), "eagles", nil, 3); err != nil {
		t.Fatal(err)
	}
	if p, ok := args[0]["providers"].([]any); !ok || len(p) != 1 || p[0] != "ytmusic--a1" {
		t.Errorf("held to one service, sent %+v", args[0])
	}
	if _, ok := args[1]["providers"]; ok {
		t.Errorf("the whole library, sent providers %+v", args[1]["providers"])
	}
}

// The services: only the running music ones, as Music Assistant lists them; and found by what people
// call them, whatever the instance is named.
func TestMusicProviders(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		json.NewDecoder(r.Body).Decode(&body)
		if body["command"] != "providers" || body["args"].(map[string]any)["provider_type"] != "music" {
			t.Errorf("asked %+v", body)
		}
		w.Write([]byte(`[{"instance_id":"ytmusic--a1","domain":"ytmusic","name":"Our YT","available":true},
			{"instance_id":"spotify--b2","domain":"spotify","name":"Spotify","available":false},
			{"instance_id":"filesystem_local--c3","domain":"filesystem_local","name":"Music folder","available":true}]`))
	}))
	defer srv.Close()
	ps, err := Client{URL: srv.URL, Token: "tok"}.MusicProviders(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(ps) != 2 {
		t.Fatalf("got %+v, want the two running ones", ps)
	}
	for said, want := range map[string]string{
		"YouTube Music": "ytmusic--a1", "youtube": "ytmusic--a1", "our yt": "ytmusic--a1",
		"ytmusic--a1": "ytmusic--a1", "music folder": "filesystem_local--c3",
	} {
		if p, ok := FindProvider(ps, said); !ok || p.InstanceID != want {
			t.Errorf("FindProvider(%q) = %+v, %v; want %s", said, p, ok, want)
		}
	}
	for _, said := range []string{"Spotify", "Tidal", ""} {
		if p, ok := FindProvider(ps, said); ok {
			t.Errorf("FindProvider(%q) found %+v, which is not running here", said, p)
		}
	}
	if d, ok := KnownDomain("Apple Music"); !ok || d != "apple_music" {
		t.Errorf("KnownDomain(Apple Music) = %q, %v", d, ok)
	}
}
