package onvifback

import (
	"bufio"
	"context"
	"net"
	"strings"
	"sync"
	"testing"
	"time"
)

// scripted is a camera that answers the first connection with answer, given each request's first line
// and headers, and records the headers it was sent.
type scripted struct {
	ln   net.Listener
	mu   sync.Mutex
	seen []string
}

func newScripted(t *testing.T, answer func(conn net.Conn, line string, hdr map[string]string)) *scripted {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	c := &scripted{ln: ln}
	t.Cleanup(func() { ln.Close() })
	go func() {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		br := bufio.NewReader(conn)
		for {
			line, err := br.ReadString('\n')
			if err != nil {
				return
			}
			hdr := map[string]string{}
			for {
				l, err := br.ReadString('\n')
				if err != nil {
					return
				}
				l = strings.TrimSpace(l)
				if l == "" {
					break
				}
				c.mu.Lock()
				c.seen = append(c.seen, l)
				c.mu.Unlock()
				k, v, _ := strings.Cut(l, ":")
				hdr[strings.ToLower(k)] = strings.TrimSpace(v)
			}
			answer(conn, line, hdr)
		}
	}()
	return c
}

func (c *scripted) url() string { return "rtsp://" + c.ln.Addr().String() + "/Preview_01_main" }

func openWithin(t *testing.T, addr string, d time.Duration) error {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), d)
	defer cancel()
	s, err := Open(ctx, addr, "admin", "secret")
	if err == nil {
		s.Close()
	}
	return err
}

// A camera that asks for the password in the clear does not get it: it is the one every camera on the
// setup page shares.
func TestThePasswordIsNeverSentInTheClear(t *testing.T) {
	cam := newScripted(t, func(conn net.Conn, line string, hdr map[string]string) {
		conn.Write([]byte("RTSP/1.0 401 Unauthorized\r\nCSeq: " + hdr["cseq"] + "\r\nWWW-Authenticate: Basic realm=\"cam\"\r\n\r\n"))
	})
	err := openWithin(t, cam.url(), 5*time.Second)
	if err == nil || !strings.Contains(err.Error(), "unprotected") {
		t.Errorf("opened with %v", err)
	}
	cam.mu.Lock()
	defer cam.mu.Unlock()
	for _, h := range cam.seen {
		if strings.HasPrefix(strings.ToLower(h), "authorization") {
			t.Errorf("sent %q", h)
		}
	}
}

// A digest this device cannot compute, or one with quotes in it, is refused rather than half-answered.
func TestAnUnusableChallengeIsRefused(t *testing.T) {
	for _, ch := range []string{
		`Digest realm="cam", nonce="abc", algorithm=SHA-256`,
		`Digest realm="cam", nonce="abc", algorithm=MD5-sess`,
	} {
		cam := newScripted(t, func(conn net.Conn, line string, hdr map[string]string) {
			conn.Write([]byte("RTSP/1.0 401 Unauthorized\r\nCSeq: " + hdr["cseq"] + "\r\nWWW-Authenticate: " + ch + "\r\n\r\n"))
		})
		if err := openWithin(t, cam.url(), 5*time.Second); err == nil || !strings.Contains(err.Error(), "cannot give") {
			t.Errorf("%s: %v", ch, err)
		}
	}
}

// A camera that never answers is let go at once when the talk is stopped, not at the deadline.
func TestStoppingWhileAskingIsImmediate(t *testing.T) {
	cam := newScripted(t, func(net.Conn, string, map[string]string) {}) // never answers
	ctx, cancel := context.WithCancel(context.Background())
	time.AfterFunc(100*time.Millisecond, cancel)
	at := time.Now()
	s, err := Open(ctx, cam.url(), "admin", "secret")
	if err == nil {
		s.Close()
		t.Fatal("opened a camera that never answered")
	}
	if took := time.Since(at); took > 2*time.Second {
		t.Errorf("a stop took %v to be noticed", took)
	}
}

// An endless line, or endless headers, is refused rather than read into memory.
func TestAnEndlessAnswerIsRefused(t *testing.T) {
	for name, body := range map[string]string{
		"line":    "RTSP/1.0 200 OK" + strings.Repeat("x", 64<<10),
		"headers": "RTSP/1.0 200 OK\r\n" + strings.Repeat("X-Pad: y\r\n", 1000),
	} {
		cam := newScripted(t, func(conn net.Conn, line string, hdr map[string]string) { conn.Write([]byte(body)) })
		if err := openWithin(t, cam.url(), 5*time.Second); err == nil || !strings.Contains(err.Error(), "too") {
			t.Errorf("%s: %v", name, err)
		}
	}
}

// A description with a broken rtpmap line does not bring the daemon down.
func TestABrokenDescriptionIsNotAPanic(t *testing.T) {
	for _, sdp := range []string{
		"v=0\r\nm=audio 0 RTP/AVP 0\r\na=control:t=1\r\na=rtpmap:\r\na=sendonly\r\n",
		"v=0\r\nm=audio\r\na=rtpmap:   \r\na=sendonly\r\n",
		"m=",
	} {
		parseSDP(sdp) // must not panic; a PCMU track by its static type may still be found
	}
}

// A backchannel whose control points at another address is refused: the login would be signed for,
// and the audio sent to, somewhere that is not the camera.
func TestATrackOnAnotherHostIsRefused(t *testing.T) {
	sdp := "v=0\r\nm=audio 0 RTP/AVP 0\r\na=control:rtsp://10.9.9.9/trackID=2\r\na=rtpmap:0 PCMU/8000\r\na=sendonly\r\n"
	cam := newFakeCamera(t, sdp)
	if err := openWithin(t, "rtsp://"+cam.ln.Addr().String()+"/Preview_01_main", 5*time.Second); err == nil || !strings.Contains(err.Error(), "another address") {
		t.Errorf("opened with %v", err)
	}
}

// The same camera written with and without RTSP's own port is the same camera; another host or port
// is not.
func TestSameHost(t *testing.T) {
	for _, c := range []struct {
		a, b string
		same bool
	}{
		{"rtsp://192.168.1.40/Preview_01_main/", "rtsp://192.168.1.40:554/h264Preview_01_main", true},
		{"rtsp://CAM.local/x", "rtsp://cam.local:554/y", true},
		{"rtsp://192.168.1.40:8554/x", "rtsp://192.168.1.40/x", false},
		{"rtsp://10.9.9.9/x", "rtsp://192.168.1.40/x", false},
	} {
		if got := sameHost(c.a, c.b); got != c.same {
			t.Errorf("%s vs %s: %v", c.a, c.b, got)
		}
	}
}
