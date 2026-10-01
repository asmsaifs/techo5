package assistant

import (
	"testing"

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
