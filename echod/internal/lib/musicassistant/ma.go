// Package musicassistant asks a Music Assistant server for music: its HTTP API, one POST to /api per
// command with a long-lived token (Music Assistant 2.7 and later). Only the few commands the voice
// assistant needs: search, and play on a player.
package musicassistant

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// Client is one server: its address (http://host:8095) and a token.
type Client struct {
	URL, Token string
}

var hc = &http.Client{Timeout: 20 * time.Second}

// do runs a command and decodes its result into out, when out is not nil.
func (c Client) do(ctx context.Context, command string, args map[string]any, out any) error {
	body, err := json.Marshal(map[string]any{"command": command, "args": args})
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(c.URL, "/")+"/api", bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+c.Token)
	resp, err := hc.Do(req)
	if err != nil {
		return fmt.Errorf("music assistant did not answer: %w", err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return err
	}
	switch resp.StatusCode {
	case http.StatusOK:
	case http.StatusUnauthorized:
		return errors.New("music assistant refused the token")
	case http.StatusForbidden:
		return errors.New("music assistant does not let this token do that")
	default:
		// The status only: what the server says about it goes to the chat model and the log otherwise.
		return fmt.Errorf("music assistant answered %s", resp.Status)
	}
	if out == nil {
		return nil
	}
	return json.Unmarshal(raw, out)
}

// Item is a piece of music the server found: an artist, album, track, playlist or radio station.
type Item struct {
	URI       string `json:"uri"`
	Name      string `json:"name"`
	MediaType string `json:"media_type"`
	Artists   []struct {
		Name string `json:"name"`
	} `json:"artists"`
}

// By is who it is by, for saying what is playing: the artists' names, or nothing.
func (i Item) By() string {
	var n []string
	for _, a := range i.Artists {
		if a.Name != "" {
			n = append(n, a.Name)
		}
	}
	return strings.Join(n, " and ")
}

// Results is a search's findings by kind.
type Results struct {
	Artists   []Item `json:"artists"`
	Albums    []Item `json:"albums"`
	Tracks    []Item `json:"tracks"`
	Playlists []Item `json:"playlists"`
	Radio     []Item `json:"radio"`
}

// Search looks for query among the given kinds (artist, album, track, playlist, radio; none means all),
// limit of each.
func (c Client) Search(ctx context.Context, query string, kinds []string, limit int) (Results, error) {
	args := map[string]any{"search_query": query, "limit": limit}
	if len(kinds) > 0 {
		args["media_types"] = kinds
	}
	var r Results
	err := c.do(ctx, "music/search", args, &r)
	return r, err
}

// Play plays uri on the player queueID, replacing what its queue held.
func (c Client) Play(ctx context.Context, queueID, uri string) error {
	return c.do(ctx, "player_queues/play_media", map[string]any{"queue_id": queueID, "media": uri, "option": "replace"}, nil)
}
