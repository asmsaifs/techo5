package onvifback

import (
	"bufio"
	"context"
	"crypto/md5"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"io"
	"math"
	"net"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/HuskerMinion/techo5/echod/internal/lib/g711"
)

// fakeCamera is a camera with a μ-law backchannel behind a digest login, which records what it is sent.
type fakeCamera struct {
	mu      sync.Mutex // methods and audio, written by the camera's goroutine
	ln      net.Listener
	methods []string
	audio   []int16
	got     chan struct{}
	sdp     string
	shy     int // descriptions to answer without the backchannel first, as a hub waking its camera does
	packets []rtpHead
}

// rtpHead is what the camera read in a packet's header.
type rtpHead struct {
	marker bool
	seq    uint16
	ts     uint32
}

const camSDP = "v=0\r\no=- 0 0 IN IP4 0.0.0.0\r\ns=cam\r\nt=0 0\r\n" +
	"m=video 0 RTP/AVP 96\r\na=control:trackID=0\r\na=recvonly\r\n" +
	"m=audio 0 RTP/AVP 97\r\na=control:trackID=1\r\na=rtpmap:97 MPEG4-GENERIC/16000\r\na=recvonly\r\n" +
	"m=audio 0 RTP/AVP 0\r\na=control:trackID=2\r\na=rtpmap:0 PCMU/8000\r\na=sendonly\r\n"

func newFakeCamera(t *testing.T, sdp string) *fakeCamera {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	c := &fakeCamera{ln: ln, got: make(chan struct{}, 1), sdp: sdp}
	go c.serve(t)
	t.Cleanup(func() { ln.Close() })
	return c
}

func (c *fakeCamera) serve(t *testing.T) {
	conn, err := c.ln.Accept()
	if err != nil {
		return
	}
	defer conn.Close()
	br := bufio.NewReader(conn)
	const realm, nonce = "cam", "abc123"
	for {
		b, err := br.Peek(1)
		if err != nil {
			return
		}
		if b[0] == '$' {
			var h [4]byte
			io.ReadFull(br, h[:])
			data := make([]byte, binary.BigEndian.Uint16(h[2:]))
			io.ReadFull(br, data)
			c.mu.Lock()
			c.packets = append(c.packets, rtpHead{data[1]&0x80 != 0, binary.BigEndian.Uint16(data[2:]), binary.BigEndian.Uint32(data[4:])})
			for _, v := range data[12:] {
				c.audio = append(c.audio, g711.ULawDecode(v))
			}
			enough := len(c.audio) >= 8000
			c.mu.Unlock()
			if enough {
				select {
				case c.got <- struct{}{}:
				default:
				}
			}
			continue
		}
		line, _ := br.ReadString('\n')
		method := strings.Fields(line)[0]
		uri := strings.Fields(line)[1]
		hdr := map[string]string{}
		for {
			l, _ := br.ReadString('\n')
			l = strings.TrimSpace(l)
			if l == "" {
				break
			}
			k, v, _ := strings.Cut(l, ":")
			hdr[strings.ToLower(k)] = strings.TrimSpace(v)
		}
		cseq := hdr["cseq"]
		c.mu.Lock()
		c.methods = append(c.methods, method)
		c.mu.Unlock()
		// The login: digest, checked properly.
		auth := hdr["authorization"]
		m := regexp.MustCompile(`response="([0-9a-f]+)"`).FindStringSubmatch(auth)
		h := func(v string) string { s := md5.Sum([]byte(v)); return hex.EncodeToString(s[:]) }
		want := h(h("admin:"+realm+":secret") + ":" + nonce + ":" + h(method+":"+uri))
		if m == nil || m[1] != want {
			fmt.Fprintf(conn, "RTSP/1.0 401 Unauthorized\r\nCSeq: %s\r\nWWW-Authenticate: Digest realm=\"%s\", nonce=\"%s\"\r\n\r\n", cseq, realm, nonce)
			continue
		}
		switch method {
		case "DESCRIBE":
			if hdr["require"] != require {
				fmt.Fprintf(conn, "RTSP/1.0 200 OK\r\nCSeq: %s\r\nContent-Length: 0\r\n\r\n", cseq)
				continue
			}
			sdp := c.sdp
			c.mu.Lock()
			if c.shy > 0 {
				c.shy--
				sdp = strings.SplitN(sdp, "m=audio 0 RTP/AVP 0", 2)[0]
			}
			c.mu.Unlock()
			fmt.Fprintf(conn, "RTSP/1.0 200 OK\r\nCSeq: %s\r\nContent-Base: %s/\r\nContent-Type: application/sdp\r\nContent-Length: %d\r\n\r\n%s", cseq, uri, len(sdp), sdp)
		case "SETUP":
			if !strings.HasSuffix(uri, "/trackID=2") {
				t.Errorf("set up %s, not the backchannel", uri)
			}
			fmt.Fprintf(conn, "RTSP/1.0 200 OK\r\nCSeq: %s\r\nSession: 12345678;timeout=60\r\nTransport: RTP/AVP/TCP;unicast;interleaved=4-5\r\n\r\n", cseq)
		case "PLAY", "GET_PARAMETER", "TEARDOWN":
			if hdr["session"] != "12345678" {
				t.Errorf("%s without the session: %q", method, hdr["session"])
			}
			fmt.Fprintf(conn, "RTSP/1.0 200 OK\r\nCSeq: %s\r\nSession: 12345678\r\n\r\n", cseq)
			// Some RTCP from the camera, a stray line end and a request of its own, which the client
			// has to read past without taking any of them for the camera hanging up.
			if method == "PLAY" {
				conn.Write([]byte{'$', 5, 0, 4, 0x80, 0xC8, 0, 0})
				fmt.Fprint(conn, "\r\nOPTIONS * RTSP/1.0\r\nCSeq: 1\r\n\r\n")
			}
		}
	}
}

// A whole session: the login asked for and given, the backchannel found among the camera's tracks, set
// up on the channel the camera chose, and a second of a tone arriving as μ-law that decodes to it.
func TestTalkingToACamera(t *testing.T) {
	cam := newFakeCamera(t, camSDP)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	s, err := Open(ctx, "rtsp://"+cam.ln.Addr().String()+"/Preview_01_main", "admin", "secret")
	if err != nil {
		t.Fatal(err)
	}
	if s.Codec() != "PCMU" || s.channel != 4 {
		t.Errorf("codec %s on channel %d, want PCMU on 4", s.Codec(), s.channel)
	}
	tone := make([]int16, 8000)
	for i := range tone {
		tone[i] = int16(8000 * math.Sin(2*math.Pi*440*float64(i)/8000))
	}
	for i := 0; i < len(tone); i += 123 { // uneven pieces: the packets are still whole
		if err := s.Write(tone[i:min(i+123, len(tone))]); err != nil {
			t.Fatal(err)
		}
	}
	select {
	case <-cam.got:
	case <-time.After(5 * time.Second):
		t.Fatal("the camera did not get a second of audio")
	}
	cam.mu.Lock()
	audio := append([]int16(nil), cam.audio[:8000]...)
	cam.mu.Unlock()
	for i, v := range audio {
		if d := int(v) - int(tone[i]); d > 600 || d < -600 {
			t.Fatalf("sample %d arrived as %d, sent %d", i, v, tone[i])
		}
	}
	select {
	case <-s.Done():
		t.Fatal("the session ended on the camera's own request")
	default:
	}
	// The packets: the first marked as the start, then numbered and timed one after another.
	cam.mu.Lock()
	pk := append([]rtpHead(nil), cam.packets...)
	cam.mu.Unlock()
	for i, p := range pk {
		if p.marker != (i == 0) {
			t.Errorf("packet %d marker %v", i, p.marker)
		}
		if i > 0 && (p.seq != pk[i-1].seq+1 || p.ts != pk[i-1].ts+PacketSamples) {
			t.Fatalf("packet %d is seq %d ts %d after %d %d", i, p.seq, p.ts, pk[i-1].seq, pk[i-1].ts)
		}
	}
	s.Close()
	cam.mu.Lock()
	defer cam.mu.Unlock()
	if got := strings.Join(cam.methods, " "); !strings.HasPrefix(got, "DESCRIBE DESCRIBE SETUP PLAY") {
		t.Errorf("the camera was sent %s", got)
	}
}

// A hub that leaves the backchannel out of its first two answers is asked again; one that leaves it
// out of three is a camera without one.
func TestHubWakingUp(t *testing.T) {
	for shy, ok := range map[int]bool{2: true, 3: false} {
		cam := newFakeCamera(t, camSDP)
		cam.mu.Lock()
		cam.shy = shy
		cam.mu.Unlock()
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		s, err := Open(ctx, "rtsp://"+cam.ln.Addr().String()+"/Preview_02_main", "admin", "secret")
		cancel()
		if ok && err != nil {
			t.Errorf("%d answers without it: %v", shy, err)
		}
		if !ok && err != errNoTalkBack {
			t.Errorf("%d answers without it: %v, want %v", shy, err, errNoTalkBack)
		}
		if s != nil {
			s.Close()
		}
	}
}

// A camera without a backchannel, or with one in a codec the device cannot send, says so.
func TestNoTalkBack(t *testing.T) {
	for sdp, want := range map[string]string{
		"v=0\r\nm=video 0 RTP/AVP 96\r\na=control:trackID=0\r\n":                                     "offers no talk-back",
		"v=0\r\nm=audio 0 RTP/AVP 98\r\na=control:t=1\r\na=rtpmap:98 opus/48000/2\r\na=sendonly\r\n": "OPUS/48000/2",
	} {
		if _, err := parseSDP(sdp); err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%q: %v, want %q", sdp, err, want)
		}
	}
}

func TestResolve(t *testing.T) {
	for _, c := range [][3]string{
		{"rtsp://cam/Preview_01_main/", "trackID=2", "rtsp://cam/Preview_01_main/trackID=2"},
		{"rtsp://cam/Preview_01_main", "trackID=2", "rtsp://cam/Preview_01_main/trackID=2"},
		{"rtsp://cam/x", "rtsp://cam/other/track", "rtsp://cam/other/track"},
		{"rtsp://cam/x", "*", "rtsp://cam/x"},
	} {
		if got := resolve(c[0], c[1]); got != c[2] {
			t.Errorf("resolve(%q, %q) = %q, want %q", c[0], c[1], got, c[2])
		}
	}
}
