package assistant

import (
	"context"
	"fmt"
	"log/slog"
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
				"query":  str("What to play: a song, artist, album or playlist name, as said, without the service's name."),
				"kind":   map[string]any{"type": "string", "enum": []string{"any", "artist", "album", "track", "playlist"}},
				"source": str("The music service named in the request, as said (\"on YouTube Music\", \"from Spotify\"); empty when none was named."),
			}, "query")},
			func(a map[string]any) (string, error) {
				return playMusic(argString(a, "query"), argString(a, "kind"), argString(a, "source"))
			}},
	}
}

func playMusic(query, kind, source string) (string, error) {
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
	f, err := findMusic(ctx, c, query, kind, source)
	if err != nil {
		return "", err
	}
	pctx, pcancel := context.WithTimeout(context.Background(), maStartWait+15*time.Second)
	defer pcancel()
	if err := playOnMA(pctx, f.pick.URI); err != nil {
		return "", err
	}
	return f.said(), nil
}

// musicPick is what findMusic chose to play, and where from, for saying so.
type musicPick struct {
	pick    musicassistant.Item
	from    musicassistant.Provider // the service asked for first; zero for none
	on      bool                    // pick came from it
	failed  bool                    // the service's own search failed, so it was never asked
	unknown string                  // a service named aloud that the server does not have
}

// findMusic searches for query. The service asked for, by voice or else by the setting, is searched
// on its own first, and what it has by exactly that name wins; else the whole library is searched,
// and what it has by that name wins; else the service's nearest, else the library's.
//
// Only an exact name takes the service's word for it: a streaming service answers nearly anything with
// something, and "Our Song" in the library should beat a cover of it there. A service search that
// fails (a token not allowed it, a service that is gone) only means the whole library is searched.
func findMusic(ctx context.Context, c musicassistant.Client, query, kind, source string) (musicPick, error) {
	kinds := []string{"artist", "album", "playlist", "track"}
	if kind != "" && kind != "any" {
		kinds = []string{kind}
	}
	var f musicPick
	f.from, f.unknown = musicSource(ctx, c, source)
	var near musicassistant.Item
	var nearOK bool
	if f.from.InstanceID != "" {
		r, err := c.Search(ctx, query, kinds, 5, f.from.InstanceID)
		if err != nil {
			slog.Warn("assistant: searching the music source", "source", f.from.InstanceID, "err", err)
			f.failed = true
		}
		// A server older than Music Assistant 2.10 searches everything whatever it is asked: only
		// what the service itself has counts as the service's.
		r = onlyFrom(r, f.from)
		if it, ok := exactMusic(r, query, kind); ok {
			f.pick, f.on = it, true
			return f, nil
		}
		near, nearOK = firstMusic(r, kind)
	}
	r, err := c.Search(ctx, query, kinds, 5)
	switch {
	case err != nil && nearOK:
		f.pick, f.on = near, true
		return f, nil
	case err != nil:
		return f, err
	}
	if it, ok := exactMusic(r, query, kind); ok {
		f.pick, f.on = it, f.from.InstanceID != "" && it.From(f.from.InstanceID, f.from.Domain)
		return f, nil
	}
	if nearOK {
		f.pick, f.on = near, true
		return f, nil
	}
	it, ok := firstMusic(r, kind)
	if !ok {
		return f, fmt.Errorf("nothing called %q was found in the music library", query)
	}
	f.pick, f.on = it, f.from.InstanceID != "" && it.From(f.from.InstanceID, f.from.Domain)
	return f, nil
}

// said is the tool's answer: what is playing, and where from when a service was asked for.
func (f musicPick) said() string {
	what := f.pick.Name
	if by := f.pick.By(); by != "" && f.pick.MediaType != "artist" {
		what += " by " + by
	}
	if w := musicWord(f.pick.MediaType); w != "" {
		what = w + " " + what
	}
	var out string
	switch {
	case f.from.InstanceID != "" && f.on:
		out = "playing " + what + " from " + f.from.Name
	case f.from.InstanceID != "" && f.failed:
		out = fmt.Sprintf("%s could not be searched just now, so playing %s from the rest of the library", f.from.Name, what)
	case f.from.InstanceID != "":
		out = fmt.Sprintf("nothing by that name on %s, so playing %s from the rest of the library", f.from.Name, what)
	default:
		out = "playing " + what
	}
	if f.unknown != "" {
		out += fmt.Sprintf("; %q is not one of the music library's services", f.unknown)
	}
	return out
}

// onlyFrom is r without what plainly came from somewhere other than p. An item that does not say
// where it came from at all (no provider, mappings or URI scheme) is kept: the server was asked for
// p's alone.
func onlyFrom(r musicassistant.Results, p musicassistant.Provider) musicassistant.Results {
	keep := func(items []musicassistant.Item) []musicassistant.Item {
		var out []musicassistant.Item
		for _, it := range items {
			if !it.Sourced() || it.From(p.InstanceID, p.Domain) {
				out = append(out, it)
			}
		}
		return out
	}
	return musicassistant.Results{Artists: keep(r.Artists), Albums: keep(r.Albums), Tracks: keep(r.Tracks),
		Playlists: keep(r.Playlists), Radio: keep(r.Radio)}
}

// anyService is what may be said for no service in particular, which leaves the setting in force.
var anyService = map[string]bool{"any": true, "anything": true, "anywhere": true, "everywhere": true, "all": true}

// musicSource is the service to search first: the one named in the request when the server has it,
// else the Music source setting's, else none. unknown is a name said that the server, asked, does not
// have: the setting is used for it all the same.
//
// A token for a user limited to this device's player may not be allowed to list the services. Then a
// spoken name is taken by the common services' own names (musicassistant.KnownDomain), and the
// setting, which is an instance id, is used as it is: Music Assistant's search takes either.
func musicSource(ctx context.Context, c musicassistant.Client, said string) (from musicassistant.Provider, unknown string) {
	said = strings.TrimSpace(said)
	if anyService[strings.ToLower(said)] {
		said = ""
	}
	setting := config.Get().MusicAssistant.Source
	if said == "" && setting == "" {
		return musicassistant.Provider{}, ""
	}
	providers, err := c.MusicProviders(ctx)
	if err != nil {
		if said != "" {
			if d, ok := musicassistant.KnownDomain(said); ok {
				return musicassistant.Provider{InstanceID: d, Domain: d, Name: said}, ""
			}
		}
		if setting != "" {
			return musicassistant.Provider{InstanceID: setting, Name: "the chosen service"}, ""
		}
		return musicassistant.Provider{}, "" // not known either way: nothing to say about it
	}
	if said != "" {
		if p, ok := musicassistant.FindProvider(providers, said); ok {
			return p, ""
		}
		unknown = said
	}
	if setting != "" {
		if p, ok := musicassistant.FindProvider(providers, setting); ok {
			return p, unknown
		}
	}
	return musicassistant.Provider{}, unknown
}

// chooseMusic is the best match: exactMusic, else firstMusic.
func chooseMusic(r musicassistant.Results, query, kind string) (musicassistant.Item, bool) {
	if it, ok := exactMusic(r, query, kind); ok {
		return it, true
	}
	return firstMusic(r, kind)
}

// musicOrder is the kinds in the order they are looked through: the one asked for, else an artist, a
// track, an album or a playlist, in that order.
func musicOrder(r musicassistant.Results, kind string) [][]musicassistant.Item {
	groups := map[string][]musicassistant.Item{"artist": r.Artists, "track": r.Tracks, "album": r.Albums, "playlist": r.Playlists}
	order := []string{"artist", "track", "album", "playlist"}
	if kind != "" && kind != "any" {
		order = []string{kind}
	}
	var out [][]musicassistant.Item
	for _, k := range order {
		out = append(out, groups[k])
	}
	return out
}

// exactMusic is one whose name is what was said, artists first.
func exactMusic(r musicassistant.Results, query, kind string) (musicassistant.Item, bool) {
	for _, g := range musicOrder(r, kind) {
		for _, it := range g {
			if strings.EqualFold(strings.TrimSpace(it.Name), strings.TrimSpace(query)) && it.URI != "" {
				return it, true
			}
		}
	}
	return musicassistant.Item{}, false
}

// firstMusic is the first of the kind asked for, else of any, by musicOrder.
func firstMusic(r musicassistant.Results, kind string) (musicassistant.Item, bool) {
	for _, g := range musicOrder(r, kind) {
		for _, it := range g {
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
