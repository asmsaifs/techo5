package camstream

import (
	"errors"
	"fmt"
	"io"
	"net"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// play logs a viewer in and asks for the camera, answering PLAY's status.
func (c *client) play() int {
	c.t.Helper()
	if r := c.do("DESCRIBE", c.url); r.status != 200 {
		c.t.Fatalf("DESCRIBE: %d", r.status)
	}
	r := c.do("SETUP", c.url+"/track1", "Transport: RTP/AVP/TCP;unicast;interleaved=0-1")
	if r.status != 200 {
		c.t.Fatalf("SETUP: %d", r.status)
	}
	id, _, _ := strings.Cut(r.headers["session"], ";")
	return c.do("PLAY", c.url, "Session: "+id).status
}

// closed is whether the server has closed a connection, reading past anything it was still sending.
func (c *client) closed(within time.Duration) bool {
	_ = c.conn.SetReadDeadline(time.Now().Add(within))
	_, err := io.Copy(io.Discard, c.r)
	var ne net.Error
	return !errors.As(err, &ne) || !ne.Timeout() // EOF or a reset; a deadline is still open
}

func TestOffEndsAPlayingStream(t *testing.T) {
	f, addr, _, released := server(t)
	c := dial(t, addr, "viewer", "secret")
	if code := c.play(); code != 200 {
		t.Fatalf("PLAY: %d", code)
	}
	f.close()
	if !c.closed(5 * time.Second) {
		t.Fatal("the stream went on after it was turned off")
	}
	select {
	case <-released:
	case <-time.After(5 * time.Second):
		t.Fatal("the camera was not let go")
	}
}

func TestANewLoginEndsTheOldOnes(t *testing.T) {
	f, addr, _, _ := server(t)
	c := dial(t, addr, "viewer", "secret")
	if code := c.play(); code != 200 {
		t.Fatalf("PLAY: %d", code)
	}
	f.dropAll() // what Poke does once the setup page has saved a login
	if !c.closed(5 * time.Second) {
		t.Fatal("a viewer logged in with the old login went on watching")
	}
}

func TestViewersCapped(t *testing.T) {
	f, addr, _, _ := server(t)
	for i := range maxViewers {
		if code := dial(t, addr, "viewer", "secret").play(); code != 200 {
			t.Fatalf("viewer %d: %d", i, code)
		}
	}
	if code := dial(t, addr, "viewer", "secret").play(); code != 453 {
		t.Fatalf("one viewer too many: %d", code)
	}
	if !f.watching() {
		t.Fatal("not watching")
	}
}

// Connections that never log in are few, and do not hold a viewer's place: one address has two places
// at most, and with every place taken the one that has waited longest is let go for a new one.
func TestStrangersCannotFillTheStream(t *testing.T) {
	_, addr, _, _ := server(t)
	open := func(from string) *client {
		c := dialFrom(t, from, addr, "", "")
		if r := c.do("OPTIONS", c.url); r.status != 200 {
			t.Fatalf("OPTIONS from %s: %d", from, r.status)
		}
		return c
	}
	first := open("127.0.0.1")
	for range maxPendingFrom - 1 {
		open("127.0.0.1")
	}
	// One address cannot take every place.
	if c := dialFrom(t, "127.0.0.1", addr, "", ""); !c.closed(5 * time.Second) {
		t.Fatal("one address took more than its places")
	}
	for range maxPending - maxPendingFrom {
		open("127.0.0.2")
	}
	// Every place taken: a new one gets in, and the one that waited longest goes.
	open("127.0.0.3")
	if !first.closed(5 * time.Second) {
		t.Fatal("the connection that waited longest was kept")
	}
}

func TestInterleavedDataBeforePlayEndsTheConnection(t *testing.T) {
	_, addr, _, _ := server(t)
	c := dial(t, addr, "", "")
	_, _ = c.conn.Write([]byte{'$', 0, 0, 2, 1, 2})
	if !c.closed(5 * time.Second) {
		t.Fatal("interleaved data taken before PLAY")
	}
}

func TestALongLineEndsTheConnection(t *testing.T) {
	_, addr, _, _ := server(t)
	c := dial(t, addr, "", "")
	go func() {
		_, _ = c.conn.Write([]byte("OPTIONS rtsp://x/camera RTSP/1.0\r\nX: " + strings.Repeat("a", 64<<10)))
	}()
	if !c.closed(5 * time.Second) {
		t.Fatal("a line without end was read on")
	}
}

func TestAnAddressIsLockedOut(t *testing.T) {
	f, addr, _, _ := server(t)
	for i := 0; i < lockAfter; i++ {
		c := dial(t, addr, "", "")
		fmt.Fprintf(c.conn, "DESCRIBE %s RTSP/1.0\r\nCSeq: 1\r\nAuthorization: Digest username=\"viewer\", realm=\"%s\", "+
			"nonce=\"stale\", uri=\"%s\", response=\"0\"\r\n\r\n", c.url, realm, c.url)
		_, _ = c.read() // a 401, or the connection closed on the one that locks the address out
		c.conn.Close()
		// The server lets the connection go before the next is made: one address may have only so many
		// waiting, and one more would be closed unread, its wrong login not counted.
		waitUntil(t, func() bool { f.mu.Lock(); defer f.mu.Unlock(); return f.waiting["127.0.0.1"] == 0 })
	}
	// The last wrong login is counted as its connection ends, a moment after its answer has gone.
	waitUntil(t, func() bool { return f.guard.refused("127.0.0.1") })
	c := dial(t, addr, "viewer", "secret")
	if !c.closed(5 * time.Second) {
		t.Fatal("a locked-out address was let in")
	}
}

func TestCSeqDigitsOnly(t *testing.T) {
	_, addr, _, _ := server(t)
	c := dial(t, addr, "", "")
	_, _ = c.conn.Write([]byte("OPTIONS " + c.url + " RTSP/1.0\r\nCSeq: 1\rX-Injected: yes\r\n\r\n"))
	r, err := c.read()
	if err != nil {
		t.Fatal(err)
	}
	if r.headers["cseq"] != "0" || r.headers["x-injected"] != "" {
		t.Fatalf("%v", r.headers)
	}
}

// A login made for another address is not taken.
func TestDigestForAnotherAddress(t *testing.T) {
	req := request{method: "DESCRIBE", uri: "rtsp://h/camera", headers: map[string]string{}}
	ha1 := md5hex("camera:" + realm + ":pw")
	other := `username="camera", realm="` + realm + `", nonce="n", uri="rtsp://h/elsewhere", response="` +
		md5hex(ha1+":n:"+md5hex("DESCRIBE:rtsp://h/elsewhere")) + `"`
	if authorized(req, "Digest "+other, "n") {
		t.Fatal("a login for another address was taken")
	}
}

// Two viewers share one start of the camera and one encode of each picture.
func TestViewersShareTheCamera(t *testing.T) {
	f, addr, pics, _ := server(t)
	var starts atomic.Int32
	inner := f.frames
	f.frames = func() (<-chan frame, func(), error) {
		starts.Add(1)
		return inner()
	}
	a, b := dial(t, addr, "viewer", "secret"), dial(t, addr, "viewer", "secret")
	if a.play() != 200 || b.play() != 200 {
		t.Fatal("PLAY")
	}
	pics <- picture(64, 48)
	for _, c := range []*client{a, b} {
		for {
			_, p := c.packet()
			if p[1]&0x80 != 0 {
				break
			}
		}
	}
	if n := starts.Load(); n != 1 {
		t.Fatalf("the camera was started %d times", n)
	}
}

// Counting an address's failures leaves them as they are: old ones are not copied over recent ones.
func TestGuardCountsWithoutChanging(t *testing.T) {
	var g guard
	now := time.Now()
	g.by = map[string]*record{"x": {fails: []time.Time{now.Add(-2 * lockWindow), now.Add(-time.Second), now.Add(-time.Second)}}}
	for range 3 {
		ok, done := g.attempt("x")
		if !ok {
			t.Fatal("refused")
		}
		done(false)
	}
	r := g.by["x"]
	if len(r.fails) != 3 || countRecent(r.fails, time.Now()) != 2 {
		t.Fatalf("failures now %v", r.fails)
	}
}

// waitUntil waits for ok, five seconds at most.
func waitUntil(t *testing.T, ok func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for !ok() {
		if time.Now().After(deadline) {
			t.Fatal("not in time")
		}
		time.Sleep(2 * time.Millisecond)
	}
}
