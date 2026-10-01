// Package radiobrowser searches Radio Browser (radio-browser.info), the free, open directory of
// internet radio that Home Assistant's own radio is built on, directly: for a device with no Home
// Assistant to ask.
package radiobrowser

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
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

// UseServers points the searches at other servers until the returned function is called: for tests
// elsewhere, which have no business reaching the real directory.
func UseServers(s ...string) (restore func()) {
	was := servers
	servers = s
	return func() { servers = was }
}

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
		// To a tenth of a degree (about 11 km): plenty for a radius of tens of kilometers, and no
		// closer to the house than that.
		v.Set("geo_lat", fmt.Sprintf("%.1f", q.Lat))
		v.Set("geo_long", fmt.Sprintf("%.1f", q.Lon))
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
	// A name may end in a dot and still be the same name: "localhost." is localhost.
	h := strings.TrimSuffix(strings.ToLower(u.Hostname()), ".")
	if h == "" || h == "localhost" || strings.HasSuffix(h, ".local") || strings.HasSuffix(h, ".localhost") {
		return false
	}
	if a, err := netip.ParseAddr(h); err == nil {
		return PublicAddr(a)
	}
	// What is not an address in the usual form but is read as one by other software (127.1,
	// 2130706433, 0x7f000001) is not a name either. A real name ends in a top-level domain, which is
	// never a number, so one whose last part is digits or 0x-hex is refused rather than guessed at.
	last := h[strings.LastIndex(h, ".")+1:]
	if strings.HasPrefix(last, "0x") || strings.Trim(last, "0123456789") == "" {
		return false
	}
	return true
}

// Public is public for others: an address handed on to be fetched by something else (the music
// library) is held to the same rule as one this device fetches.
func Public(raw string) bool { return public(raw) }

// nonPublic are the ranges that are no one's on the internet, beyond what netip names: the shared
// space that carrier networks and many VPNs number their devices from, "this
// network", and the benchmarking range.
var nonPublic = []netip.Prefix{
	netip.MustParsePrefix("100.64.0.0/10"),
	netip.MustParsePrefix("0.0.0.0/8"),
	netip.MustParsePrefix("198.18.0.0/15"),
}

// PublicAddr is whether an address is one on the internet, rather than the home network's, this
// device's own, or a VPN's.
func PublicAddr(a netip.Addr) bool {
	a = a.Unmap()
	if a.IsPrivate() || a.IsLoopback() || a.IsLinkLocalUnicast() || a.IsUnspecified() || a.IsMulticast() {
		return false
	}
	for _, p := range nonPublic {
		if p.Contains(a) {
			return false
		}
	}
	return true
}

// Resolves is whether every address a public station address's name stands for right now is a public
// one too: a name in the directory can point into the home (192-168-1-1.nip.io) as easily as an
// address can. For an address handed to something else to fetch; it can still be changed after it is
// asked, so it narrows the door rather than shutting it.
func Resolves(ctx context.Context, raw string) bool {
	if !public(raw) {
		return false
	}
	u, _ := url.Parse(raw)
	h := strings.TrimSuffix(u.Hostname(), ".")
	if _, err := netip.ParseAddr(h); err == nil {
		return true
	}
	addrs, err := net.DefaultResolver.LookupNetIP(ctx, "ip", h)
	if err != nil || len(addrs) == 0 {
		return false
	}
	for _, a := range addrs {
		if !PublicAddr(a) {
			return false
		}
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
