package reolink

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

// A recorder that logs in, lists two channels, refuses to scale snapshots, and forgets its token once.
func fakeRecorder(t *testing.T) (*httptest.Server, *atomic.Int32) {
	var logins atomic.Int32
	var lapsed atomic.Bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cmd := r.URL.Query().Get("cmd")
		token := r.URL.Query().Get("token")
		switch cmd {
		case "Login":
			body, _ := io.ReadAll(r.Body)
			if !strings.Contains(string(body), `"password":"right"`) {
				_, _ = w.Write([]byte(`[{"cmd":"Login","code":1,"error":{"detail":"login failed","rspCode":-7}}]`))
				return
			}
			n := logins.Add(1)
			_ = json.NewEncoder(w).Encode([]map[string]any{{"cmd": "Login", "code": 0,
				"value": map[string]any{"Token": map[string]any{"name": "tok" + string(rune('0'+n)), "leaseTime": 3600}}}})
		case "GetChannelstatus":
			_, _ = w.Write([]byte(`[{"cmd":"GetChannelstatus","code":0,"value":{"count":2,"status":[
			 {"channel":0,"name":"Driveway","online":1},{"channel":1,"name":"","online":0}]}}]`))
		case "Snap":
			if token == "tok1" && !lapsed.Swap(true) {
				_, _ = w.Write([]byte(`[{"cmd":"Snap","code":1,"error":{"detail":"please login first","rspCode":-6}}]`))
				return
			}
			if r.URL.Query().Get("width") != "" {
				_, _ = w.Write([]byte(`[{"cmd":"Snap","code":1,"error":{"detail":"param error","rspCode":-4}}]`))
				return
			}
			_, _ = w.Write([]byte{0xff, 0xd8, 0xff, 0xe0, 1, 2, 3})
		}
	}))
	t.Cleanup(srv.Close)
	return srv, &logins
}

func TestChannelsAndASnapshot(t *testing.T) {
	srv, logins := fakeRecorder(t)
	c := &Client{Base: srv.URL, User: "admin", Pass: "right"}
	chs, err := c.Channels(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(chs) != 2 || chs[0].Name != "Driveway" || !chs[0].Online || chs[1].Name != "Camera 2" {
		t.Errorf("channels = %+v", chs)
	}
	// The first token has lapsed and the recorder will not scale: the picture still comes, full size,
	// with a token logged in for again.
	b, err := c.Snap(context.Background(), 0, 640)
	if err != nil {
		t.Fatal(err)
	}
	if b[0] != 0xff || b[1] != 0xd8 {
		t.Error("not a JPEG")
	}
	if n := logins.Load(); n != 2 {
		t.Errorf("logged in %d times, want 2", n)
	}
}

func TestAWrongPasswordIsSaidSo(t *testing.T) {
	srv, _ := fakeRecorder(t)
	c := &Client{Base: srv.URL, User: "admin", Pass: "wrong"}
	if err := c.login(context.Background()); err != errLogin {
		t.Errorf("login with the wrong password: %v", err)
	}
}
