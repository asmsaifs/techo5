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
	"net/netip"
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
	Codec       string // MP3: what the device can decode itself
	// Near is stations within RadiusKm of Lat, Lon, which the directory knows for the stations that
	// say where they are.
	Near        bool
	Lat, Lon    float64
	RadiusKm    float64
	ByListeners bool // most listened to first (clickcount), rather than most voted
	Limit       int
}

// Search asks the first mirror that answers, most voted first, working streams only.
func Search(ctx context.Context, q Query) ([]Station, error) {
	v := url.Values{"hidebroken": {"true"}, "order": {"votes"}, "reverse": {"true"}}
	if q.ByListeners {
		v.Set("order", "clickcount")
	}
	limit := q.Limit
	if limit <= 0 {
		limit = 8
	}
	v.Set("limit", fmt.Sprint(limit))
	for k, val := range map[string]string{"name": q.Name, "tag": strings.ToLower(q.Tag), "countrycode": q.CountryCode, "state": q.State, "codec": q.Codec} {
		if val != "" {
			v.Set(k, val)
		}
	}
	if q.Near {
		v.Set("geo_lat", fmt.Sprintf("%.4f", q.Lat))
		v.Set("geo_long", fmt.Sprintf("%.4f", q.Lon))
		v.Set("geo_distance", fmt.Sprintf("%.0f", q.RadiusKm*1000)) // meters
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

// public is whether a station's address is one on the internet: http or https, to a name or an address
// that is not the home network's. Anyone can add a station to the directory, and the device plays what
// it finds, so an entry pointing into the home (a router's page, another device) is left out.
func public(raw string) bool {
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Hostname() == "" || u.User != nil {
		return false
	}
	h := strings.ToLower(u.Hostname())
	if h == "localhost" || strings.HasSuffix(h, ".local") || strings.HasSuffix(h, ".localhost") {
		return false
	}
	if a, err := netip.ParseAddr(h); err == nil {
		a = a.Unmap()
		return !(a.IsPrivate() || a.IsLoopback() || a.IsLinkLocalUnicast() || a.IsUnspecified() || a.IsMulticast())
	}
	return true
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
		if name == "" || !public(u) {
			continue
		}
		out = append(out, Station{Name: name, URL: u, Tags: r.Tags, State: r.State, Country: r.Country,
			Codec: r.Codec, Bitrate: r.Bitrate, Votes: r.Votes})
	}
	return out, nil
}
