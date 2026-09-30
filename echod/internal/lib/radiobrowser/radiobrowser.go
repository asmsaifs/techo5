// Package radiobrowser searches Radio Browser (radio-browser.info), the free, open directory of
// internet radio that Home Assistant's own radio is built on, directly: for a device with no Home
// Assistant to ask.
package radiobrowser

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// servers are Radio Browser's mirrors, tried in turn.
var servers = []string{"https://de1.api.radio-browser.info", "https://de2.api.radio-browser.info",
	"https://fi1.api.radio-browser.info", "https://nl1.api.radio-browser.info"}

var client = &http.Client{Timeout: 12 * time.Second}

// Station is one stream.
type Station struct {
	Name, URL, Tags, State, Country, Codec string
	Bitrate, Votes                         int
}

// Query narrows a search. Empty fields are not asked for.
type Query struct {
	Name        string // part of the station's name
	Tag         string // a genre: country, jazz, news
	CountryCode string // US
	State       string // Texas
	Limit       int
}

// Search asks the first mirror that answers, most voted first, working streams only.
func Search(ctx context.Context, q Query) ([]Station, error) {
	v := url.Values{"hidebroken": {"true"}, "order": {"votes"}, "reverse": {"true"}}
	limit := q.Limit
	if limit <= 0 {
		limit = 8
	}
	v.Set("limit", fmt.Sprint(limit))
	for k, val := range map[string]string{"name": q.Name, "tag": strings.ToLower(q.Tag), "countrycode": q.CountryCode, "state": q.State} {
		if val != "" {
			v.Set(k, val)
		}
	}
	var last error
	for _, s := range servers {
		out, err := get(ctx, s+"/json/stations/search?"+v.Encode())
		if err == nil {
			return out, nil
		}
		last = err
		if ctx.Err() != nil {
			break
		}
	}
	return nil, last
}

func get(ctx context.Context, u string) ([]Station, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	// Radio Browser asks every client to say what it is.
	req.Header.Set("User-Agent", "TECHO5/1.0 (github.com/HuskerMinion/techo5)")
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("radio browser: %s", resp.Status)
	}
	var raw []struct {
		Name        string `json:"name"`
		URLResolved string `json:"url_resolved"`
		URL         string `json:"url"`
		Tags        string `json:"tags"`
		State       string `json:"state"`
		Country     string `json:"country"`
		Codec       string `json:"codec"`
		Bitrate     int    `json:"bitrate"`
		Votes       int    `json:"votes"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 4<<20)).Decode(&raw); err != nil {
		return nil, err
	}
	var out []Station
	for _, r := range raw {
		u := r.URLResolved
		if u == "" {
			u = r.URL
		}
		name := strings.Join(strings.Fields(r.Name), " ")
		if name == "" || u == "" {
			continue
		}
		out = append(out, Station{Name: name, URL: u, Tags: r.Tags, State: r.State, Country: r.Country,
			Codec: r.Codec, Bitrate: r.Bitrate, Votes: r.Votes})
	}
	return out, nil
}
