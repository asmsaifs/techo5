package go2rtc

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestBase(t *testing.T) {
	for in, want := range map[string]string{
		"192.0.2.7":                    "http://192.0.2.7:1984",
		"192.0.2.7:1985":               "http://192.0.2.7:1985",
		"https://cams.example:443/x?y": "https://cams.example:443",
		"http://u:p@192.0.2.7":         "http://u:p@192.0.2.7:1984",
		"go2rtc.local":                 "http://go2rtc.local:1984",
	} {
		u, err := Base(in)
		if err != nil || u.String() != want {
			t.Errorf("%q: %v %v, want %s", in, u, err, want)
		}
	}
	for _, bad := range []string{"", "ftp://x", "http://"} {
		if _, err := Base(bad); err == nil {
			t.Errorf("%q made an address", bad)
		}
	}
}

func TestStreamsFrameAndRTSP(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/streams":
			_, _ = w.Write([]byte(`{"porch":{"producers":[]},"drive":{"producers":[]}}`))
		case "/api/frame.jpeg":
			if r.URL.Query().Get("src") != "porch" || r.URL.Query().Get("width") != "320" {
				http.Error(w, "bad", http.StatusBadRequest)
				return
			}
			_, _ = w.Write([]byte("jpeg"))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	c, err := New(srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	names, err := c.Streams(context.Background())
	if err != nil || strings.Join(names, ",") != "drive,porch" {
		t.Errorf("streams %v %v", names, err)
	}
	if b, err := c.Frame(context.Background(), "porch", 320); err != nil || string(b) != "jpeg" {
		t.Errorf("frame %q %v", b, err)
	}
	if _, err := c.Frame(context.Background(), "nothing", 0); err == nil {
		t.Error("a frame of a stream that is not there")
	}
	c2, _ := New("http://u:p@192.0.2.7")
	if got := c2.RTSP("porch"); got != "rtsp://u:p@192.0.2.7:8554/porch" {
		t.Errorf("rtsp %s", got)
	}
}

// An address that is refused is not repeated: it may hold a password.
func TestBaseDoesNotRepeatAPassword(t *testing.T) {
	_, err := Base("ftp://admin:s3cret@192.0.2.7:1984")
	if err == nil || strings.Contains(err.Error(), "s3cret") {
		t.Errorf("%v", err)
	}
}

// A long list is cut short, and names that cannot be a camera's are left out.
func TestStreamsAreCappedAndClean(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var b strings.Builder
		b.WriteString(`{"":{},"bad\u0007name":{},"evil\u202ename":{},"` + strings.Repeat("x", 100) + `":{}`)
		for i := range MaxStreams + 50 {
			fmt.Fprintf(&b, `,"cam%03d":{}`, i)
		}
		b.WriteString("}")
		_, _ = w.Write([]byte(b.String()))
	}))
	defer srv.Close()
	c, _ := New(srv.URL)
	names, err := c.Streams(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(names) != MaxStreams {
		t.Errorf("%d streams, want %d", len(names), MaxStreams)
	}
	for _, n := range names {
		if !strings.HasPrefix(n, "cam") {
			t.Errorf("kept %q", n)
		}
	}
}

// A redirect to another server is not followed: the login is not sent there.
func TestARedirectElsewhereIsNotFollowed(t *testing.T) {
	other := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("the redirect was followed")
	}))
	defer other.Close()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, other.URL+"/api/streams", http.StatusFound)
	}))
	defer srv.Close()
	c, _ := New(strings.Replace(srv.URL, "http://", "http://u:p@", 1))
	if _, err := c.Streams(context.Background()); err == nil {
		t.Error("a redirect elsewhere was taken")
	}
}
