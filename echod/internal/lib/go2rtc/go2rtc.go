// Package go2rtc reads a go2rtc server (github.com/AlexxIT/go2rtc): the streams it has, a picture of one,
// and the RTSP address it serves one at. go2rtc is what many homes already run beside Home Assistant to
// turn every camera, whatever it speaks, into streams anything can read, so a device that reads go2rtc
// reads them all.
package go2rtc

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

const (
	// APIPort and RTSPPort are go2rtc's own, unless an address says otherwise.
	APIPort  = "1984"
	RTSPPort = "8554"

	// maxList is more than any go2rtc's list, and maxFrame more than any picture of one.
	maxList  = 1 << 20
	maxFrame = 16 << 20

	// MaxStreams is the most streams taken from a list, and maxName the longest name kept: each is a
	// camera on every list the device shows.
	MaxStreams = 128
	maxName    = 64
)

// Client is one go2rtc server.
type Client struct {
	base *url.URL
	http *http.Client
}

// New is a client for the server at address: a host, host:port or http(s) URL, with a user and password
// in it if the server has them.
func New(address string) (*Client, error) {
	u, err := Base(address)
	if err != nil {
		return nil, err
	}
	return &Client{base: u, http: &http.Client{
		Timeout: 15 * time.Second,
		// A login goes to the server it was given for and nowhere a redirect points.
		CheckRedirect: func(req *http.Request, _ []*http.Request) error {
			if req.URL.Scheme != u.Scheme || req.URL.Host != u.Host {
				return fmt.Errorf("go2rtc redirected to another server")
			}
			return nil
		},
	}}, nil
}

// Base is address made a server's URL: http when no scheme is given, go2rtc's own port when no port is,
// and no path. Its errors never repeat the address, which may hold a password.
func Base(address string) (*url.URL, error) {
	address = strings.TrimSpace(address)
	if address == "" {
		return nil, fmt.Errorf("no address")
	}
	if !strings.Contains(address, "://") {
		address = "http://" + address
	}
	u, err := url.Parse(address)
	if err != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") {
		return nil, fmt.Errorf("that is not a go2rtc server's address: it takes a host, host:port or an http or https address")
	}
	if u.Port() == "" {
		u.Host = net.JoinHostPort(u.Hostname(), APIPort)
	}
	u.Path, u.RawQuery, u.Fragment = "", "", ""
	return u, nil
}

// Streams is the names of the streams the server has, sorted.
func (c *Client) Streams(ctx context.Context) ([]string, error) {
	b, err := c.get(ctx, "/api/streams", nil, maxList, "a list")
	if err != nil {
		return nil, err
	}
	var streams map[string]json.RawMessage
	if err := json.Unmarshal(b, &streams); err != nil {
		return nil, fmt.Errorf("go2rtc's list could not be read: %w", err)
	}
	names := make([]string, 0, len(streams))
	for name := range streams {
		if usable(name) {
			names = append(names, name)
		}
	}
	slices.Sort(names)
	if len(names) > MaxStreams {
		names = names[:MaxStreams]
	}
	return names, nil
}

// usable is whether a stream's name can be a camera's: something, not too long, and nothing in it that is
// not text.
func usable(name string) bool {
	if name == "" || utf8.RuneCountInString(name) > maxName || !utf8.ValidString(name) {
		return false
	}
	for _, r := range name {
		// Control characters, and the invisible ones that change how text around them reads (a right-to-left
		// override, say), have no place in a name shown on a list.
		if unicode.IsControl(r) || unicode.Is(unicode.Cf, r) {
			return false
		}
	}
	return true
}

// Frame is a picture of a stream, as a JPEG, about width wide when width is more than zero.
func (c *Client) Frame(ctx context.Context, stream string, width int) ([]byte, error) {
	q := url.Values{"src": {stream}}
	if width > 0 {
		q.Set("width", fmt.Sprint(width))
	}
	return c.get(ctx, "/api/frame.jpeg", q, maxFrame, "a picture")
}

// RTSP is the address the server serves a stream at, with the server's own user and password when its
// address has them.
func (c *Client) RTSP(stream string) string {
	u := url.URL{Scheme: "rtsp", Host: net.JoinHostPort(c.base.Hostname(), RTSPPort), Path: "/" + stream, User: c.base.User}
	return u.String()
}

func (c *Client) get(ctx context.Context, path string, q url.Values, limit int64, what string) ([]byte, error) {
	u := *c.base
	u.Path, u.RawQuery = path, q.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, err
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("go2rtc did not answer: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("go2rtc said %s", resp.Status)
	}
	b, err := io.ReadAll(io.LimitReader(resp.Body, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(b)) > limit {
		return nil, fmt.Errorf("go2rtc sent more than %s", what)
	}
	return b, nil
}
