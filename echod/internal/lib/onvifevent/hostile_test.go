package onvifevent

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// A recorder is whatever answers at its address. These are the ways one could answer badly.

// A redirect is never followed: a redirected POST would carry the digest to wherever it pointed.
func TestRedirectNotFollowed(t *testing.T) {
	var elsewhere atomic.Int32
	other := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { elsewhere.Add(1) }))
	defer other.Close()
	for _, code := range []int{http.StatusMovedPermanently, http.StatusFound, http.StatusTemporaryRedirect, http.StatusPermanentRedirect} {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			http.Redirect(w, r, other.URL+"/onvif/device_service", code)
		}))
		for _, hc := range []*http.Client{nil, {Timeout: 5 * time.Second}} {
			c := &Client{Base: srv.URL, User: "admin", Pass: "secret", HTTP: hc}
			if _, err := c.Subscribe(context.Background(), time.Minute); err == nil {
				t.Errorf("%d: subscribed through a redirect", code)
			}
		}
		srv.Close()
	}
	if n := elsewhere.Load(); n != 0 {
		t.Fatalf("the redirect's target was asked %d times", n)
	}
}

// Addresses in a reply that point somewhere else are asked on the recorder's own host.
func TestAddressesStayOnTheRecorder(t *testing.T) {
	var elsewhere atomic.Int32
	other := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { elsewhere.Add(1) }))
	defer other.Close()
	ou, _ := url.Parse(other.URL)
	var pulled atomic.Bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.ReadAll(r.Body)
		ct := r.Header.Get("Content-Type")
		switch {
		case strings.Contains(ct, "GetCapabilities"):
			io.WriteString(w, `<Envelope><Body><Capabilities><Events><XAddr>@`+ou.Host+`/onvif/event_service</XAddr></Events></Capabilities></Body></Envelope>`)
		case strings.Contains(ct, "CreatePullPoint"):
			io.WriteString(w, `<Envelope><Body><SubscriptionReference><Address>@`+ou.Host+`/pull</Address></SubscriptionReference></Body></Envelope>`)
		case strings.Contains(ct, "PullMessages"):
			pulled.Store(true)
			io.WriteString(w, `<Envelope><Body/></Envelope>`)
		default:
			io.WriteString(w, `<Envelope><Body/></Envelope>`)
		}
	}))
	defer srv.Close()
	c := &Client{Base: srv.URL, User: "admin", Pass: "secret"}
	s, err := c.Subscribe(context.Background(), time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Pull(context.Background(), time.Second); err != nil {
		t.Fatal(err)
	}
	if !pulled.Load() || elsewhere.Load() != 0 {
		t.Fatalf("pulled on the recorder %v, asked elsewhere %d times", pulled.Load(), elsewhere.Load())
	}
}

// A reply that never ends is read up to maxReply and no further; broken XML is an error, not a hang
// or a panic; and a reply with more events than were asked for gives only those asked for.
func TestHostileReplies(t *testing.T) {
	endless := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.ReadAll(r.Body)
		io.WriteString(w, `<Envelope><Body><PullMessagesResponse>`)
		chunk := strings.Repeat(`<NotificationMessage><Topic>x</Topic></NotificationMessage>`, 1000)
		for range 200 { // ~12 MB, far past the cap
			if _, err := io.WriteString(w, chunk); err != nil {
				return
			}
		}
	}))
	defer endless.Close()
	c := &Client{Base: endless.URL, HTTP: &http.Client{Timeout: 10 * time.Second}}
	s := &Subscription{c: c, addr: endless.URL + "/pull"}
	evs, _ := s.Pull(context.Background(), time.Second)
	if len(evs) > maxEvents {
		t.Errorf("%d events from one reply, want at most %d", len(evs), maxEvents)
	}

	broken := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.ReadAll(r.Body)
		io.WriteString(w, `<Envelope><Body><PullMessagesResponse><NotificationMessage><Topic>tns1:x</Top`)
	}))
	defer broken.Close()
	s = &Subscription{c: &Client{Base: broken.URL}, addr: broken.URL + "/pull"}
	if _, err := s.Pull(context.Background(), time.Second); err == nil {
		t.Error("broken XML read without an error")
	}

	if evs, err := parseEvents([]byte(`<a><NotificationMessage><Message PropertyOperation="Changed"><SimpleItem Name="State" Value="true"/></Message></NotificationMessage>` +
		strings.Repeat(`<NotificationMessage/>`, 100) + `</a>`)); err != nil || len(evs) != maxEvents || evs[0].Items["State"] != "true" {
		t.Errorf("parseEvents gave %d events, err %v", len(evs), err)
	}
}
