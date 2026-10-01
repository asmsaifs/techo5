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
