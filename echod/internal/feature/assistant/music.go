package assistant

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/HuskerMinion/techo5/echod/internal/config"
	"github.com/HuskerMinion/techo5/echod/internal/lib/llm"
	"github.com/HuskerMinion/techo5/echod/internal/lib/musicassistant"
)

// Music by voice from a Music Assistant server (config.MusicAssistant): "play some Eagles" searches it
// and plays what it found on this device, which the server knows as a Sendspin player by the device's
// factory MAC. Offered only where a server is set up; the radio stays the radio tools'.

func musicTools() []tool {
	if !config.Get().MusicAssistant.Set() {
		return nil
	}
	return []tool{
		{llm.Tool{Name: "play_music", Description: "Play music from the house's music library: a song, an artist, an album or a playlist, by name. Not for radio stations.",
			Parameters: object(map[string]any{
				"query": str("What to play: a song, artist, album or playlist name, as said."),
				"kind":  map[string]any{"type": "string", "enum": []string{"any", "artist", "album", "track", "playlist"}},
			}, "query")},
			func(a map[string]any) (string, error) { return playMusic(argString(a, "query"), argString(a, "kind")) }},
	}
}

func playMusic(query, kind string) (string, error) {
	query = strings.TrimSpace(query)
	if query == "" {
		return "", fmt.Errorf("nothing was named to play")
	}
	c, _, err := maClient()
	if err != nil {
		return "", err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	kinds := []string{"artist", "album", "playlist", "track"}
	if kind != "" && kind != "any" {
		kinds = []string{kind}
	}
	r, err := c.Search(ctx, query, kinds, 5)
	if err != nil {
		return "", err
	}
	pick, ok := chooseMusic(r, query, kind)
	if !ok {
		return "", fmt.Errorf("nothing called %q was found in the music library", query)
	}
	pctx, pcancel := context.WithTimeout(context.Background(), maStartWait+15*time.Second)
	defer pcancel()
	if err := playOnMA(pctx, pick.URI); err != nil {
		return "", err
	}
	what := pick.Name
	if by := pick.By(); by != "" && pick.MediaType != "artist" {
		what += " by " + by
	}
	if w := musicWord(pick.MediaType); w != "" {
		what = w + " " + what
	}
	return "playing " + what, nil
}

// chooseMusic is the best match: one whose name is what was said, artists first; else the first of the
// kind asked for; else an artist, a track, an album or a playlist, in that order.
func chooseMusic(r musicassistant.Results, query, kind string) (musicassistant.Item, bool) {
	groups := map[string][]musicassistant.Item{"artist": r.Artists, "track": r.Tracks, "album": r.Albums, "playlist": r.Playlists}
	order := []string{"artist", "track", "album", "playlist"}
	if kind != "" && kind != "any" {
		order = []string{kind}
	}
	for _, k := range order {
		for _, it := range groups[k] {
			if strings.EqualFold(strings.TrimSpace(it.Name), query) && it.URI != "" {
				return it, true
			}
		}
	}
	for _, k := range order {
		for _, it := range groups[k] {
			if it.URI != "" {
				return it, true
			}
		}
	}
	return musicassistant.Item{}, false
}

func musicWord(mediaType string) string {
	switch mediaType {
	case "artist":
		return "music by"
	case "album":
		return "the album"
	case "playlist":
		return "the playlist"
	}
	return ""
}
