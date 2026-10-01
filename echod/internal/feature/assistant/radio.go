package assistant

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/HuskerMinion/techo5/echod/internal/config"
	"github.com/HuskerMinion/techo5/echod/internal/feature/home"
	"github.com/HuskerMinion/techo5/echod/internal/lib/llm"
	"github.com/HuskerMinion/techo5/echod/internal/lib/radiobrowser"
)

// The radio by voice: the stations the device already has (its own, and Home Assistant's lists where
// there is a Home Assistant), and any other found in Radio Browser, the open directory Home Assistant's
// radio is itself built on, so a device with no Home Assistant can still be asked for "some country"
// or "WGN". What was found is remembered for the conversation, so "play the second one" and "save it"
// work.

var found struct {
	sync.Mutex
	list []radiobrowser.Station
	// by is how list was searched: by "name" or by "genre".
	by string
}

func radioTools() []tool {
	return []tool{
		{llm.Tool{Name: "find_radio", Description: "Find radio stations to play: by name or call letters (WGN), or by genre (country, news, jazz). Genres are searched near this device first.",
			Parameters: object(map[string]any{
				"query": str("A station's name or call letters, or a genre."),
				"by":    map[string]any{"type": "string", "enum": []string{"name", "genre"}},
			}, "query", "by")},
			func(a map[string]any) (string, error) {
				ss, err := findRadio(argString(a, "query"), argString(a, "by"))
				if err != nil {
					return "", err
				}
				if len(ss) == 0 {
					return "no stations found", nil
				}
				var s []string
				for i, st := range ss {
					line := fmt.Sprintf("%d. %s", i+1, st.Name)
					if st.State != "" {
						line += " (" + st.State + ")"
					}
					if t := firstTags(st.Tags, 3); t != "" {
						line += ": " + t
					}
					s = append(s, line)
				}
				return strings.Join(s, "\n"), nil
			}},

		{llm.Tool{Name: "play_radio", Description: "Play a radio station: one this device has, or one find_radio found (by its name). A station not found yet is looked up by name.",
			Parameters: object(map[string]any{"station": str("The station's name.")}, "station")},
			func(a map[string]any) (string, error) { return playRadio(argString(a, "station")) }},

		{llm.Tool{Name: "save_station", Description: "Keep a station on this device, so its radio page offers it to tap: one find_radio found, by name.",
			Parameters: object(map[string]any{"station": str("The station's name.")}, "station")},
			func(a map[string]any) (string, error) {
				st, ok := foundStation(argString(a, "station"))
				if !ok {
					return "", fmt.Errorf("no station called %q was found; use find_radio first", argString(a, "station"))
				}
				if err := home.SaveStation(st.Name, st.URL); err != nil {
					return "", err
				}
				return "saved " + st.Name + " on this device", nil
			}},

		{llm.Tool{Name: "list_stations", Description: "List the radio stations this device already has.",
			Parameters: object(map[string]any{})},
			func(map[string]any) (string, error) {
				r := home.Get().Radio()
				if len(r.Stations) == 0 {
					return "no stations kept on this device; use find_radio", nil
				}
				return strings.Join(r.Stations, "; "), nil
			}},
	}
}

// findRadio searches Radio Browser. A genre is looked for in the device's state, then its country,
// since few stations say what state they are in.
func findRadio(q, by string) ([]radiobrowser.Station, error) {
	if q == "" {
		return nil, fmt.Errorf("nothing to look for")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	p := config.Get().Home.Place
	country := p.Country
	if country == "" {
		country = "US"
	}
	state := ""
	if _, st, ok := strings.Cut(p.Name, ", "); ok {
		state = st
	}
	var ss []radiobrowser.Station
	var err error
	// MP3 only: a station found here is played by the device itself, which decodes nothing else.
	const codec = "MP3"
	if by == "genre" {
		if state != "" {
			ss, err = radiobrowser.Search(ctx, radiobrowser.Query{Tag: q, CountryCode: country, State: state, Codec: codec, Limit: 6})
		}
		if err == nil && len(ss) < 3 {
			var more []radiobrowser.Station
			more, err = radiobrowser.Search(ctx, radiobrowser.Query{Tag: q, CountryCode: country, Codec: codec, Limit: 8})
			ss = mergeStations(ss, more, 6)
		}
	} else {
		ss, err = radiobrowser.Search(ctx, radiobrowser.Query{Name: q, CountryCode: country, Codec: codec, Limit: 6})
		if err == nil && len(ss) == 0 {
			ss, err = radiobrowser.Search(ctx, radiobrowser.Query{Name: q, Codec: codec, Limit: 6})
		}
	}
	if err != nil {
		return nil, fmt.Errorf("the radio directory did not answer")
	}
	found.Lock()
	found.list, found.by = ss, by
	found.Unlock()
	return ss, nil
}

func mergeStations(a, b []radiobrowser.Station, n int) []radiobrowser.Station {
	out := a
	for _, s := range b {
		dup := false
		for _, o := range out {
			if o.URL == s.URL || strings.EqualFold(o.Name, s.Name) {
				dup = true
				break
			}
		}
		if !dup {
			out = append(out, s)
		}
		if len(out) >= n {
			break
		}
	}
	return out
}

// foundStation is one of the stations last found, by name, a part of it, or its number in the list.
func foundStation(name string) (radiobrowser.Station, bool) {
	found.Lock()
	defer found.Unlock()
	w := strings.ToLower(strings.TrimSpace(name))
	if w == "" {
		return radiobrowser.Station{}, false
	}
	for _, s := range found.list {
		if strings.ToLower(s.Name) == w {
			return s, true
		}
	}
	for i, s := range found.list {
		if strings.Contains(strings.ToLower(s.Name), w) || fmt.Sprint(i+1) == w {
			return s, true
		}
	}
	return radiobrowser.Station{}, false
}

func playRadio(name string) (string, error) {
	if name == "" {
		return "", fmt.Errorf("no station was named")
	}
	// The device's own lists first: what it already plays, the way its radio page plays it. One it
	// plays itself is checked; one Home Assistant plays is its to report.
	if s, ok := station(name); ok {
		if url, direct := home.Get().StreamOf(s); direct {
			if err := home.Get().PlayStreamChecked(s, url, playCheck); err != nil {
				return "", fmt.Errorf("%s did not play: %v", s, err)
			}
			return "playing " + s, nil
		}
		home.Get().Play(s)
		return "playing " + s, nil
	}
	st, ok := foundStation(name)
	if !ok {
		if _, err := findRadio(name, "name"); err != nil {
			return "", err
		}
		if st, ok = foundStation(name); !ok {
			found.Lock()
			if len(found.list) > 0 {
				st, ok = found.list[0], true
			}
			found.Unlock()
		}
	}
	if !ok {
		return "", fmt.Errorf("no station called %q was found", name)
	}
	// The one asked for, then others from the same search: a station that is down or sends something
	// the device cannot play is common, and the next one usually works. From a genre search any of its
	// stations will do; from a search by name, only another stream of the same station, since a
	// different station is not what was asked for.
	tries := []radiobrowser.Station{st}
	found.Lock()
	for _, o := range found.list {
		if len(tries) == 3 {
			break
		}
		if o.URL == st.URL || (found.by != "genre" && !sameStation(o.Name, st.Name)) {
			continue
		}
		tries = append(tries, o)
	}
	found.Unlock()
	var failed []string
	for _, t := range tries {
		err := home.Get().PlayStreamChecked(t.Name, t.URL, playCheck)
		if err == nil {
			switch {
			case len(failed) == 0:
				return "playing " + t.Name, nil
			case sameStation(t.Name, st.Name):
				return "playing " + t.Name + " (its first stream did not play)", nil
			}
			return fmt.Sprintf("playing %s instead (%s did not play)", t.Name, st.Name), nil
		}
		if !slices.Contains(failed, t.Name) {
			failed = append(failed, t.Name)
		}
	}
	return "", fmt.Errorf("none of these played: %s", strings.Join(failed, ", "))
}

// playCheck is how long a station has to make a sound before it counts as not playing.
const playCheck = 10 * time.Second

// sameStation is whether two directory names are the same station: one's words within the other's.
func sameStation(a, b string) bool {
	a, b = strings.ToLower(strings.TrimSpace(a)), strings.ToLower(strings.TrimSpace(b))
	return a != "" && b != "" && (strings.Contains(a, b) || strings.Contains(b, a))
}

func firstTags(tags string, n int) string {
	var out []string
	for _, t := range strings.Split(tags, ",") {
		if t = strings.TrimSpace(t); t != "" {
			out = append(out, t)
		}
		if len(out) == n {
			break
		}
	}
	return strings.Join(out, ", ")
}
