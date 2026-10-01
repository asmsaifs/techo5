package musicassistant

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
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
