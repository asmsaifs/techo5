package assistant

import (
	"context"
	"fmt"
	"regexp"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/HuskerMinion/techo5/echod/internal/config"
	"github.com/HuskerMinion/techo5/echod/internal/feature/home"
	"github.com/HuskerMinion/techo5/echod/internal/feature/media"
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
					if where := strings.Trim(st.State+", "+st.Country, ", "); where != "" {
						line += " (" + where + ")"
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

// findRadio searches Radio Browser, nearest first: a station by name, call letters or frequency is
// looked for within reach of this device, then in its state, then its country, and only then
// anywhere, since a station of the same name or on the same frequency elsewhere is not the one meant.
// A genre is looked for in the state, then the country, since few stations say where they are.
//
// With a music library set up, any stream will do: what the device cannot decode itself the library
// converts for it (playRadio). Without one, only the streams the device plays itself are offered.
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
	codec := "MP3"
	if libraryReady() {
		codec = ""
	}
	// "106.7 FM" is named "106.7 The Fox" in the directory, which looks for the words as written.
	q = strings.TrimSpace(frequencySuffix.ReplaceAllString(q, "$1"))
	var queries []radiobrowser.Query
	if by == "genre" {
		if state != "" {
			queries = append(queries, radiobrowser.Query{Tag: q, CountryCode: country, State: state, Codec: codec, Limit: 6})
		}
		queries = append(queries, radiobrowser.Query{Tag: q, CountryCode: country, Codec: codec, Limit: 8})
	} else {
		if p.Set() {
			queries = append(queries, radiobrowser.Query{Name: q, Codec: codec, Near: true, Lat: p.Lat, Lon: p.Lon, RadiusKm: nearKm, Limit: 6})
		}
		if state != "" {
			queries = append(queries, radiobrowser.Query{Name: q, CountryCode: country, State: state, Codec: codec, Limit: 6})
		}
		// A frequency is a local thing: 106.7 elsewhere is another station altogether, so one is
		// looked for only here, and not finding it is the answer.
		if !isFrequency(q) {
			queries = append(queries,
				radiobrowser.Query{Name: q, CountryCode: country, Codec: codec, Limit: 6},
				radiobrowser.Query{Name: q, Codec: codec, Limit: 6})
		}
	}
	var ss []radiobrowser.Station
	var err error
	for _, qq := range queries {
		var more []radiobrowser.Station
		more, err = radiobrowser.Search(ctx, qq)
		if err != nil {
			break
		}
		ss = mergeStations(ss, more, 6)
		// Nearer results are enough on their own: farther ones would only outvote them. A genre
		// wants a few to choose from.
		if (by != "genre" && len(ss) > 0) || len(ss) >= 3 {
			break
		}
	}
	if err != nil && len(ss) == 0 {
		return nil, fmt.Errorf("the radio directory did not answer")
	}
	found.Lock()
	found.list, found.by = ss, by
	found.Unlock()
	return ss, nil
}

// frequencyWords is a station asked for by its frequency alone: "106.7", "106.7 FM", "1080 AM".
var frequencyWords = regexp.MustCompile(`(?i)^\s*\d{2,4}(\.\d)?\s*(fm|am)?\s*$`)

// frequencySuffix is a frequency's band, which names in the directory seldom carry as said.
var frequencySuffix = regexp.MustCompile(`(?i)^(\s*\d{2,4}(?:\.\d)?)\s*(?:fm|am)\s*$`)

func isFrequency(q string) bool { return frequencyWords.MatchString(q) }

// nearKm is how far a station can be and still be local: a big city's stations carry farther, but a
// search by name or frequency within this is very likely the station meant.
const nearKm = 160

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
		if isFrequency(name) {
			return "", fmt.Errorf("no station on %s was found near this device; try its call letters or name", name)
		}
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
	var why error
	// One voice turn waits for all of this: past radioPatience, what has not played is said instead.
	until := time.Now().Add(radioPatience)
	for _, t := range tries {
		if time.Now().After(until) {
			break
		}
		err := playStation(t)
		if err == nil {
			name := t.Name + whereFrom(t)
			switch {
			case len(failed) == 0:
				return "playing " + name, nil
			case sameStation(t.Name, st.Name):
				return "playing " + name + " (its first stream did not play)", nil
			}
			return fmt.Sprintf("playing %s instead (%s did not play)", name, st.Name), nil
		}
		why = err
		if !slices.Contains(failed, t.Name) {
			failed = append(failed, t.Name)
		}
	}
	return "", fmt.Errorf("none of these played: %s (%v)", strings.Join(failed, ", "), why)
}

// libraryReady is media.LibraryReady; a variable for the tests, which have no player of their own.
var libraryReady = media.LibraryReady

// radioPatience is the longest a request for a station tries others before saying none played.
const radioPatience = 35 * time.Second

// playStation plays one directory stream: an MP3 one on the device itself, which decodes it; any other
// through the music library, which converts it, where there is one.
func playStation(t radiobrowser.Station) error {
	if strings.EqualFold(t.Codec, "MP3") {
		return home.Get().PlayStreamChecked(t.Name, t.URL, playCheck)
	}
	if !config.Get().MusicAssistant.Set() {
		return fmt.Errorf("its stream is %s, which this device cannot play without a music library", t.Codec)
	}
	if !libraryReady() {
		return fmt.Errorf("its stream is %s, which this device can play only through the music library, and that is not connected right now", t.Codec)
	}
	ctx, cancel := context.WithTimeout(context.Background(), maStartWait+20*time.Second)
	defer cancel()
	// The library fetches it from where the library is: only an address on the internet is handed
	// on, never one a directory entry points into a home with.
	if !radiobrowser.Resolves(ctx, t.URL) {
		return fmt.Errorf("its stream address is not a public one")
	}
	return playOnMA(ctx, t.URL)
}

// whereFrom is where a station is, said when it is not in this device's own state, so that one of
// the same name far away is not taken for the local one.
func whereFrom(t radiobrowser.Station) string {
	p := config.Get().Home.Place
	_, state, _ := strings.Cut(p.Name, ", ")
	// Where the device is not known closely enough to compare, nothing is said about where a station is.
	if state == "" || (t.State != "" && strings.EqualFold(t.State, state)) {
		return ""
	}
	if where := strings.Trim(t.State+", "+t.Country, ", "); where != "" {
		return " from " + where
	}
	return ""
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
