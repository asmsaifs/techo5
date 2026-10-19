package announce

import (
	"bytes"
	"context"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/HuskerMinion/techo5/echod/internal/config"
)

// shareRig is this device as "Office" in a house with a word, taking whatever it is sent.
func shareRig(t *testing.T) (took *[]string) {
	t.Helper()
	config.Use(filepath.Join(t.TempDir(), "state.json"))
	if err := config.Set().Home().HouseWord("pickled herring"); err != nil {
		t.Fatal(err)
	}
	config.Started(config.Device{Name: "Office"})
	var got []string
	wasOpen, wasRecv := ShareOpen, ShareReceive
	ShareOpen = func() bool { return true }
	ShareReceive = func(from string, body []byte) ([]byte, error) {
		got = append(got, from+":"+string(body))
		return []byte(`{"results":{}}`), nil
	}
	shareTries.Lock()
	shareTries.by = nil
	shareTries.Unlock()
	t.Cleanup(func() { ShareOpen, ShareReceive = wasOpen, wasRecv })
	return &got
}

func shareRequest(header, from string, body []byte) *http.Request {
	r := httptest.NewRequest(http.MethodPost, sharePath, bytes.NewReader(body))
	r.Header.Set(shareAuthHeader, header)
	r.Header.Set(shareFromHeader, url.QueryEscape(from))
	r.RemoteAddr = "192.0.2.7:4000"
	return r
}

func serveShare(r *http.Request) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	receiveShare(w, r)
	return w
}

func TestShareSignedIsTaken(t *testing.T) {
	took := shareRig(t)
	body := []byte(`{"kind":"copy"}`)
	w := serveShare(shareRequest(shareSign("pickled herring", "Kitchen", "Office", body, time.Now()), "Kitchen", body))
	if w.Code != http.StatusOK || len(*took) != 1 || (*took)[0] != `Kitchen:{"kind":"copy"}` {
		t.Fatalf("%d %v", w.Code, *took)
	}
}

func TestShareRefusals(t *testing.T) {
	word, body, now := "pickled herring", []byte(`{"kind":"copy"}`), time.Now()
	for _, c := range []struct {
		name string
		req  func() *http.Request
		code int
	}{
		{"another word", func() *http.Request {
			return shareRequest(shareSign("other word", "Kitchen", "Office", body, now), "Kitchen", body)
		}, http.StatusForbidden},
		{"the body changed", func() *http.Request {
			return shareRequest(shareSign(word, "Kitchen", "Office", body, now), "Kitchen", []byte(`{"kind":"set"}`))
		}, http.StatusForbidden},
		{"signed for another device", func() *http.Request {
			return shareRequest(shareSign(word, "Kitchen", "Bedroom", body, now), "Kitchen", body)
		}, http.StatusForbidden},
		{"passed off as from another device", func() *http.Request {
			return shareRequest(shareSign(word, "Kitchen", "Office", body, now), "Bedroom", body)
		}, http.StatusForbidden},
		{"no signature", func() *http.Request { return shareRequest("", "Kitchen", body) }, http.StatusForbidden},
		{"a malformed one", func() *http.Request { return shareRequest("share-v1 1 2 3", "Kitchen", body) }, http.StatusForbidden},
		{"another clock", func() *http.Request {
			return shareRequest(shareSign(word, "Kitchen", "Office", body, now.Add(-time.Hour)), "Kitchen", body)
		}, http.StatusConflict},
		{"a get", func() *http.Request { return httptest.NewRequest(http.MethodGet, sharePath, nil) }, http.StatusMethodNotAllowed},
	} {
		t.Run(c.name, func(t *testing.T) {
			took := shareRig(t)
			w := serveShare(c.req())
			if w.Code != c.code || len(*took) != 0 {
				t.Fatalf("%d (want %d), taken %v", w.Code, c.code, *took)
			}
			if c.code == http.StatusConflict && w.Header().Get(timeHeader) == "" {
				t.Error("no clock sent back")
			}
		})
	}
}

func TestShareReplayRefused(t *testing.T) {
	took := shareRig(t)
	body := []byte(`{"kind":"copy"}`)
	h := shareSign("pickled herring", "Kitchen", "Office", body, time.Now())
	serveShare(shareRequest(h, "Kitchen", body))
	if w := serveShare(shareRequest(h, "Kitchen", body)); w.Code != http.StatusForbidden || len(*took) != 1 {
		t.Fatalf("a replay: %d, taken %d", w.Code, len(*took))
	}
}

func TestShareTurnsAwayAnAddressThatKeepsFailing(t *testing.T) {
	shareRig(t)
	body := []byte(`{}`)
	for range shareFailures {
		serveShare(shareRequest("share-v1 1 2 3", "Kitchen", body))
	}
	good := shareRequest(shareSign("pickled herring", "Kitchen", "Office", body, time.Now()), "Kitchen", body)
	if w := serveShare(good); w.Code != http.StatusTooManyRequests {
		t.Fatalf("%d", w.Code)
	}
}

// SendShare reaches a device found on the network, and signs again by its clock when told ours is out.
func TestSendShareAcrossClocks(t *testing.T) {
	took := shareRig(t)
	srv := httptest.NewServer(http.HandlerFunc(receiveShare))
	defer srv.Close()
	host, port, _ := net.SplitHostPort(strings.TrimPrefix(srv.URL, "http://"))
	p, _ := strconv.Atoi(port)
	peers.Lock()
	was := peers.by
	peers.by = map[string]known{"office": {Peer: Peer{Name: "Office", Address: host, Port: p}, at: time.Now()}}
	peers.Unlock()
	defer func() { peers.Lock(); peers.by = was; peers.Unlock() }()

	wasNow := receiveNow
	receiveNow = func() time.Time { return time.Now().Add(time.Hour) }
	defer func() { receiveNow = wasNow }()

	reply, err := SendShare(context.Background(), "office", []byte(`{"kind":"copy"}`))
	if err != nil {
		t.Fatal(err)
	}
	if string(reply) != `{"results":{}}` || len(*took) != 1 {
		t.Fatalf("reply %q, taken %v", reply, *took)
	}
	if _, err := SendShare(context.Background(), "Garage", []byte(`{}`)); err == nil {
		t.Error("sent to a device that is not on the network")
	}
}

// A device with sharing off answers as though there were nothing there: said as not taking, not as gone.
func TestSendShareToADeviceNotTaking(t *testing.T) {
	shareRig(t)
	srv := httptest.NewServer(http.NotFoundHandler())
	defer srv.Close()
	host, port, _ := net.SplitHostPort(strings.TrimPrefix(srv.URL, "http://"))
	p, _ := strconv.Atoi(port)
	peers.Lock()
	was := peers.by
	peers.by = map[string]known{"office": {Peer: Peer{Name: "Office", Address: host, Port: p}, at: time.Now()}}
	peers.Unlock()
	defer func() { peers.Lock(); peers.by = was; peers.Unlock() }()
	if _, err := SendShare(context.Background(), "Office", []byte(`{}`)); !errors.Is(err, ErrShareNotTaking) {
		t.Fatalf("%v", err)
	}
}

// A reply is believed only signed for the message it answers: one from anything that answered for the
// device without the house word, or signed for another message, is refused.
func TestSendShareRefusesAnUnsignedReply(t *testing.T) {
	shareRig(t)
	for name, h := range map[string]http.HandlerFunc{
		"no signature": func(w http.ResponseWriter, r *http.Request) {
			_, _ = w.Write([]byte(`{"results":{"bass":"older"},"current":{"bass":{"id":"bass","value":"6","n":99}}}`))
		},
		"signed for another message": func(w http.ResponseWriter, r *http.Request) {
			body := []byte(`{"results":{}}`)
			w.Header().Set(shareReplyHeader, shareReplyMAC("pickled herring", "some other nonce", "Office", "Office", body))
			_, _ = w.Write(body)
		},
	} {
		t.Run(name, func(t *testing.T) {
			srv := httptest.NewServer(h)
			defer srv.Close()
			host, port, _ := net.SplitHostPort(strings.TrimPrefix(srv.URL, "http://"))
			p, _ := strconv.Atoi(port)
			peers.Lock()
			was := peers.by
			peers.by = map[string]known{"office": {Peer: Peer{Name: "Office", Address: host, Port: p}, at: time.Now()}}
			peers.Unlock()
			defer func() { peers.Lock(); peers.by = was; peers.Unlock() }()
			if reply, err := SendShare(context.Background(), "Office", []byte(`{}`)); err == nil {
				t.Fatalf("believed %q", reply)
			}
		})
	}
}
