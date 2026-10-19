package remote

import (
	"bufio"
	"context"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/HuskerMinion/techo5/echod/internal/config"
)

// A request without even the shape of a signature is turned away before anything of it is read.
func TestNothingIsReadFromAnUnsignedRequest(t *testing.T) {
	for _, h := range []string{"", "v1", "v1 1 2", "v2 1 2 3", "v1 1 2 short"} {
		if looksSigned(h) {
			t.Errorf("%q looks signed", h)
		}
	}
	if !looksSigned(Sign(testKey, "GET", "/p", nil, time.Now())) {
		t.Error("a real signature does not look signed")
	}
}

// A global IPv6 host is counted by its /64, so it cannot step past the limit by changing its address;
// one on the home's own link or private range by itself, so it cannot turn the rest of the home away.
func TestFailuresAreCountedByNetworkForIPv6(t *testing.T) {
	src := func(addr string) string {
		r := httptest.NewRequest("GET", "/", nil)
		r.RemoteAddr = addr
		return source(r)
	}
	if src("[2001:db8:1:2::10]:4000") != src("[2001:db8:1:2::99]:4001") {
		t.Error("one global /64 counted apart")
	}
	if src("[fe80::10]:4000") == src("[fe80::99]:4000") {
		t.Error("two link-local hosts counted as one")
	}
	if src("[fd00:1:2:3::10]:4000") == src("[fd00:1:2:3::99]:4000") {
		t.Error("two private hosts counted as one")
	}
	if src("192.0.2.5:4000") != "192.0.2.5" {
		t.Errorf("an IPv4 address counted as %q", src("192.0.2.5:4000"))
	}
}

// A stream outlives the time a request's body is given to arrive.
func TestAStreamOutlivesTheBodysDeadline(t *testing.T) {
	f, _, _ := testRemote(t)
	was := bodyWithin
	bodyWithin = 200 * time.Millisecond
	t.Cleanup(func() { bodyWithin = was })
	path := filepath.Join(t.TempDir(), "techo5.log")
	if err := os.WriteFile(path, []byte("before\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	lines, cancel := followLog(t, f, path)
	defer cancel()
	<-lines
	time.Sleep(3 * bodyWithin)
	fh, _ := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0)
	_, _ = fh.WriteString("after\n")
	fh.Close()
	select {
	case got, open := <-lines:
		if !open || got != "after" {
			t.Errorf("after the deadline: %q, open %v", got, open)
		}
	case <-time.After(3 * time.Second):
		t.Error("nothing came after the deadline")
	}
}

// followLog follows the log at path with the API as it is, and answers the lines as they come.
func followLog(t *testing.T, f *Remote, path string) (<-chan string, context.CancelFunc) {
	t.Helper()
	was, wasRed := logPath, newRedactor
	logPath = path
	newRedactor = func() func(string) string { return func(s string) string { return s } }
	t.Cleanup(func() { logPath, newRedactor = was, wasRed })

	srv := httptest.NewServer(http.HandlerFunc(f.serve))
	t.Cleanup(srv.Close)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	req, _ := http.NewRequestWithContext(ctx, "GET", srv.URL+"/api/v1/logs", nil)
	req.Header.Set(authHeader, Sign(testKey, "GET", "/api/v1/logs", nil, time.Now()))
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		cancel()
		t.Fatal(err)
	}
	lines := make(chan string, 16)
	go func() {
		defer resp.Body.Close()
		defer close(lines)
		sc := bufio.NewScanner(resp.Body)
		for sc.Scan() {
			if s, ok := strings.CutPrefix(sc.Text(), "data: "); ok {
				lines <- s
			}
		}
	}()
	return lines, cancel
}

// A carriage return inside a line does not split it into more than one event.
func TestALineIsOneEvent(t *testing.T) {
	f, _, _ := testRemote(t)
	path := filepath.Join(t.TempDir(), "techo5.log")
	if err := os.WriteFile(path, []byte("one\rid: 7\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	lines, cancel := followLog(t, f, path)
	defer cancel()
	if got := <-lines; got != "oneid: 7" {
		t.Errorf("sent %q", got)
	}
}

// A new key ends a log being followed with the old one.
func TestANewKeyEndsTheStream(t *testing.T) {
	f, _, _ := testRemote(t)
	path := filepath.Join(t.TempDir(), "techo5.log")
	if err := os.WriteFile(path, []byte("before\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	lines, cancel := followLog(t, f, path)
	defer cancel()
	<-lines
	if err := config.Set().Remote().Secret(SecretOf("another key")); err != nil {
		t.Fatal(err)
	}
	deadline := time.After(3 * time.Second)
	for {
		select {
		case _, open := <-lines:
			if !open {
				return
			}
		case <-deadline:
			t.Fatal("the stream went on after a new key was made")
		}
	}
}

// A request turned away before its body is in is answered at once, however slowly the body comes, and
// its connection let go: neither the answer nor the end of the handler waits on a body that is not
// going to arrive.
func TestASlowBodyIsAnsweredAtOnce(t *testing.T) {
	f, _, _ := testRemote(t)
	was := bodyWithin
	bodyWithin = 300 * time.Millisecond
	t.Cleanup(func() { bodyWithin = was })
	closed := make(chan struct{}, 8)
	srv := httptest.NewUnstartedServer(http.HandlerFunc(f.serve))
	srv.Config.ConnState = func(_ net.Conn, s http.ConnState) {
		if s == http.StateClosed {
			closed <- struct{}{}
		}
	}
	srv.Start()
	defer srv.Close()

	for name, header := range map[string]string{
		"unsigned":             "",
		"slow, signed-looking": "X-Techo5-Remote: " + Sign(testKey, "POST", "/api/v1/entities/x", nil, time.Now()) + "\r\n",
	} {
		conn, err := net.Dial("tcp", srv.Listener.Addr().String())
		if err != nil {
			t.Fatal(err)
		}
		fmt.Fprintf(conn, "POST /api/v1/entities/x HTTP/1.1\r\nHost: d\r\n%sContent-Length: 1000\r\n\r\nab", header)
		_ = conn.SetReadDeadline(time.Now().Add(3 * time.Second))
		line, err := bufio.NewReader(conn).ReadString('\n')
		if err != nil || !strings.HasPrefix(line, "HTTP/1.1 4") {
			t.Errorf("%s: %q, %v; want an answer at once", name, line, err)
		}
		select {
		case <-closed:
		case <-time.After(2 * time.Second):
			t.Errorf("%s: the server still held the connection after answering", name)
		}
		conn.Close()
	}
}

// A request signed at a time this device would not take, or before it started, is turned away before
// its body is read; so is one when the few places for unchecked bodies are all taken.
func TestTimeAndPlaceAreCheckedBeforeTheBody(t *testing.T) {
	now := time.Now()
	h := func(at time.Time) string {
		return "v1 " + strconv.FormatInt(at.Unix(), 10) + " n " + strings.Repeat("a", 64)
	}
	if !inTime(h(now), now) {
		t.Error("a request signed now is out of time")
	}
	if inTime(h(now.Add(-2*skew)), now) || inTime(h(now.Add(2*skew)), now) {
		t.Error("a request outside the skew is in time")
	}
	was := started
	started = now
	t.Cleanup(func() { started = was })
	if inTime(h(now.Add(-2*time.Second)), now) {
		t.Error("a request signed before the start is in time")
	}
	// The clock set back an hour after the start: a request signed by it is still in time.
	if back := now.Add(-time.Hour); !inTime(h(back), back) {
		t.Error("a clock set back after the start refuses everything")
	}
	for range maxUnverified {
		unverified <- struct{}{}
	}
	t.Cleanup(func() {
		for range maxUnverified {
			<-unverified
		}
	})
	f := &Remote{}
	req := httptest.NewRequest(http.MethodGet, prefix+"device", nil)
	req.Header.Set(authHeader, h(time.Now()))
	req.RemoteAddr = "192.0.2.77:1234"
	w := httptest.NewRecorder()
	f.serve(w, req)
	if w.Code != http.StatusServiceUnavailable {
		t.Errorf("with every place taken: %d, want 503", w.Code)
	}
}

// Requests signed by a clock that is out are refused, and not counted against the address: a script
// whose clock is wrong is not locked out once it is put right.
func TestAClockOutIsNotAGuesser(t *testing.T) {
	f, _, _ := testRemote(t)
	for range failures + 1 {
		req := httptest.NewRequest("GET", "/api/v1/device", nil)
		req.RemoteAddr = "192.0.2.50:4000"
		req.Header.Set(authHeader, Sign(testKey, "GET", "/api/v1/device", nil, time.Now().Add(-time.Hour)))
		w := httptest.NewRecorder()
		f.serve(w, req)
		if w.Code != http.StatusUnauthorized {
			t.Fatalf("a request an hour out: %d", w.Code)
		}
	}
	if w := call(f, "GET", "/api/v1/device", "", true); w.Code != http.StatusOK {
		t.Errorf("after a clock that was out: %d %s", w.Code, w.Body)
	}
}
