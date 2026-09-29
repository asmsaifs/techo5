package xiaozhi

// A cloud to talk to, and a client with a session open to it.
//
// The uplink is the first part of this client that carries audio, and audio is the one thing here
// that cannot be checked by reading the code: what matters is the shape of the packets that come
// out of the encoder, and a packet is bytes. So the cloud below records what arrives rather than
// answering, and the assertions are about what it was sent.

import (
	"context"
	"encoding/json"
	"math"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/tphakala/go-opus/opus"

	"github.com/HuskerMinion/techo5/echod/internal/config"
	"github.com/HuskerMinion/techo5/echod/internal/hardware/mic"
)

// cloud is a server that behaves like the official one for everything a turn touches: the OTA
// call, the WebSocket, the hello, and a record of everything the client then sends.
type cloud struct {
	url  string
	gone chan struct{}

	mu     sync.Mutex
	text   [][]byte
	audio  [][]byte
	opened int
}

// serve answers the OTA call and then every upgrade after it.
func (c *cloud) serve(t *testing.T) {
	t.Helper()

	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == OTAPath {
			// The WebSocket is named by the endpoint rather than worked out from the host, or this
			// is not standing in for the cloud but for something easier to reach.
			body, _ := json.Marshal(map[string]any{
				"websocket": map[string]string{
					"url":   "ws" + strings.TrimPrefix(srv.URL, "http") + "/xiaozhi/v1/",
					"token": "t-" + r.Header.Get("Device-Id"),
				},
			})
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write(body)
			return
		}

		up := websocket.Upgrader{}
		conn, err := up.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer func() { _ = conn.Close() }()

		// The hello has to be read before the answer, or the client's read of it is not a message
		// from the server and the rest of the test races for a reason that is not the client's.
		_ = conn.SetReadDeadline(time.Now().Add(30 * time.Second))
		var hello map[string]any
		if err := conn.ReadJSON(&hello); err != nil {
			return
		}
		if err := conn.WriteJSON(map[string]any{
			"type": TypeHello, "transport": "websocket", "session_id": "s-test",
			// The rate is the server's, and it is not the one this client sends. A decoder built
			// from the wrong one is a chipmunk, and M3 is where that would be caught.
			"audio_params": map[string]any{
				"format": "opus", "sample_rate": defaultDownlinkRate, "channels": 1, "frame_duration": 60,
			},
		}); err != nil {
			return
		}

		c.mu.Lock()
		c.opened++
		c.mu.Unlock()

		_ = conn.SetReadDeadline(time.Now().Add(30 * time.Second))
		for {
			kind, data, err := conn.ReadMessage()
			if err != nil {
				close(c.gone)
				return
			}
			frame := append([]byte(nil), data...)

			c.mu.Lock()
			if kind == websocket.BinaryMessage {
				c.audio = append(c.audio, frame)
			} else {
				c.text = append(c.text, frame)
			}
			c.mu.Unlock()
		}
	}))
	c.url = srv.URL
	t.Cleanup(srv.Close)
}

// messages is the JSON the client sent, decoded.
func (c *cloud) messages(t *testing.T) []Event {
	t.Helper()
	c.mu.Lock()
	raw := append([][]byte(nil), c.text...)
	c.mu.Unlock()

	var out []Event
	for _, b := range raw {
		var e Event
		if err := json.Unmarshal(b, &e); err != nil {
			t.Errorf("the client sent something that is not a message: %q", b)
			continue
		}
		out = append(out, e)
	}
	return out
}

// packets is the binary audio the client sent.
func (c *cloud) packets() [][]byte {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([][]byte(nil), c.audio...)
}

// waitAudio waits for n packets, and returns them. A write that has returned is on the wire and
// not yet read, so a test that looked immediately after would be reading the server's record
// rather than the client's behaviour.
func (c *cloud) waitAudio(t *testing.T, what string, n int) [][]byte {
	t.Helper()
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		c.mu.Lock()
		got := len(c.audio)
		c.mu.Unlock()
		if got >= n {
			return c.packets()
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("the client sent %d of %d packets the %s", len(c.packets()), n, what)
	return nil
}

// waitText waits for n messages, and returns them decoded.
func (c *cloud) waitText(t *testing.T, what string, n int) []Event {
	t.Helper()
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		c.mu.Lock()
		got := len(c.text)
		c.mu.Unlock()
		if got >= n {
			return c.messages(t)
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("the client sent %d of %d messages the %s", len(c.text), n, what)
	return nil
}

// ws is the address the OTA call handed out, which is where the socket actually went.
func (c *cloud) ws() string { return "ws" + strings.TrimPrefix(c.url, "http") + "/xiaozhi/v1/" }

// newSession opens one session against a cloud and hands back the client half of it. The caller
// owns the settings; nothing here reads or writes them.
func newSession(t *testing.T) (*Session, *cloud) {
	t.Helper()

	c := &cloud{gone: make(chan struct{})}
	c.serve(t)

	sess, err := dial(context.Background(), c.ws(), "test-token",
		identity{DeviceID: "b0:f7:c4:ff:e5:92", ClientID: "6b2f7e9c-3d1a-4c58-9f0e-2a1b3c4d5e6f"})
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	t.Cleanup(func() { _ = sess.Close() })

	go func() { _ = sess.Serve(context.Background(), func(Event) {}) }()
	return sess, c
}

// The plan's central claim, checked against bytes rather than against intent: what the uplink
// sends is CELT-only, sixty milliseconds, mono, at the bitrate that was asked for.
//
// CELT is the whole risk the plan rested on. libopus would have chosen SILK for speech, so a
// change of encoder would change this silently, and a test that only counted packets would not
// notice. Noticing is the point.
func TestUplinkSendsCELTOnlySixtyMillisecondFrames(t *testing.T) {
	restore(t)

	// The measured row, which is what a device runs with and what M0 was verified at. If this
	// assertion fails then every number below is being measured at the wrong settings.
	if x := config.Get().Xiaozhi; x.Bitrate != 24000 || x.Complexity != 4 || x.Frame() != 60 {
		t.Fatalf("the defaults are %d bps, complexity %d, %d ms: the numbers below were measured "+
			"at 24 kbps, 4 and 60", x.Bitrate, x.Complexity, x.Frame())
	}
	const kbps = 24000

	sess, c := newSession(t)
	up, err := newUplink(sess)
	if err != nil {
		t.Fatalf("newUplink: %v", err)
	}

	// Thirty 20 ms frames is exactly ten 60 ms packets. A whole number is what makes the remainder
	// assertable: a second of audio is sixteen and two thirds of a packet, and the two thirds is
	// correctly dropped rather than padded out, which would leave the byte count below measuring
	// that drop instead of the bitrate.
	for range 30 {
		if err := up.feed(tone(mic.FrameSamples)); err != nil {
			t.Fatalf("feed: %v", err)
		}
	}
	got := c.waitAudio(t, "600 ms of audio", 10)

	if len(got) != 10 {
		t.Errorf("%d packets for 600 ms of audio, want 10: the frame is not the %d ms the "+
			"settings asked for", len(got), config.Get().Xiaozhi.Frame())
	}
	if pending := up.pending(); pending != 0 {
		t.Errorf("%d samples left over, want none: 600 ms is ten whole packets", pending)
	}

	var total int
	for i, pkt := range got {
		if len(pkt) == 0 {
			t.Fatalf("packet %d is empty", i)
		}
		mode, _, sub, channels := opus.ParseTOC(pkt[0])
		if mode != opus.ModeCELTOnly {
			t.Errorf("packet %d is %v, want CELT: the server decodes with libopus, which would "+
				"have coded narrowband speech as SILK, and whether it follows a CELT stream is "+
				"the one thing this client cannot check for itself", i, mode)
		}
		if channels != 1 {
			t.Errorf("packet %d is %v, want mono", i, channels)
		}
		// The TOC's frame size is the sub-frame and not the packet, which is the one thing here
		// that looks like a bug and is not: 60 ms of CELT is three 20 ms frames concatenated, and
		// libopus says 20 ms in the header of a packet that holds 60 ms of audio. The duration
		// below is the claim; this is the shape it has to be in to be one.
		if sub != opus.FrameDuration20ms {
			t.Errorf("packet %d holds %v sub-frames, want 20 ms", i, sub)
		}
		// The whole packet's length, in the units the question is answered in. Asked at 48 kHz
		// because that is what libopus reports, whatever the packet was coded at.
		if n, err := opus.PacketDuration(pkt); err != nil || n != 60*48000/1000 {
			t.Errorf("packet %d lasts %d samples at 48 kHz (%v), want %d for 60 ms", i, n, err, 60*48000/1000)
		}
		total += len(pkt)
	}

	// Constant bitrate is the other half of the claim: the plan measured VBR at 20 ms overshooting
	// the rate it was asked for by nearly twofold, and CBR is exact at both frame sizes. 600 ms at
	// 24 kbps is 1800 bytes, and twenty either way of tolerance is a packet's worth of drift rather
	// than a rate that is wrong.
	if want := kbps / 8 * 6 / 10; total < want-20 || total > want+20 {
		t.Errorf("%d bytes for 600 ms at %d bps, want about %d", total, kbps, want)
	}
}

// handEdit writes the codec settings the way they reach the device in practice: somebody editing
// the state file. There is no setter for them on purpose, because 60 ms at complexity four is the
// row of the table in the plan that was measured, and a control surface for the others would be a
// way off it. The file is the input, so the file is what a test sets.
//
// The settings are read back out of the file rather than passed in afterwards, so a test cannot
// go on to check a configuration the device would never hold.
func handEdit(t *testing.T, bitrate, frameMS int) {
	t.Helper()
	if bitrate <= 0 {
		bitrate = config.DefaultXiaozhiBitrate
	}
	body, err := json.Marshal(map[string]any{
		"xiaozhi": map[string]int{"bitrate": bitrate, "frame_ms": frameMS},
	})
	if err != nil {
		t.Fatal(err)
	}
	path := t.TempDir() + "/state.json"
	if err := os.WriteFile(path, body, 0o644); err != nil {
		t.Fatal(err)
	}
	config.Use(path)

	x := config.Get().Xiaozhi
	if x.Bitrate != bitrate || x.FrameMS != frameMS {
		t.Fatalf("the edit did not take: %d bps, %d ms", x.Bitrate, x.FrameMS)
	}
}

// The bitrate setting has to be what decides the size of a packet, or the table in the plan is a
// table about a number nothing reads.
func TestUplinkHonoursTheBitrateSetting(t *testing.T) {
	size := func(kbps int) int {
		t.Helper()
		handEdit(t, kbps, 60)

		sess, c := newSession(t)
		up, err := newUplink(sess)
		if err != nil {
			t.Fatalf("newUplink at %d bps: %v", kbps, err)
		}
		// 600 ms is ten whole packets at 60 ms.
		for range 30 {
			if err := up.feed(tone(mic.FrameSamples)); err != nil {
				t.Fatalf("feed: %v", err)
			}
		}
		return len(c.waitAudio(t, "600 ms of audio", 10)[0])
	}

	low, high := size(16000), size(24000)
	// 60 ms is 120 bytes at 16 kbps and 180 at 24 kbps. Asserted as the ratio between them rather
	// than as those two numbers: the claim under test is that the setting is read, and the exact
	// byte count is a property of the codec that was measured on the device, not decided here.
	if high*2 != low*3 {
		t.Errorf("packets are %d bytes at 16 kbps and %d at 24 kbps, which is not 3:2", low, high)
	}
}

// A frame length the encoder does not have would fail on every frame, from inside the codec, with
// an error that does not name the setting. It is refused once here instead, and the refusal says
// what the choices are, because the person who typed the number is reading a log.
func TestTheFrameSettingIsRefusedRatherThanFailingPerFrame(t *testing.T) {
	handEdit(t, 0, 30)

	sess, _ := newSession(t)
	_, err := newUplink(sess)
	if err == nil {
		t.Fatal("a 30 ms frame was accepted")
	}
	for _, want := range []string{"30 ms", "20, 40, 60"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("the refusal %q does not mention %q", err, want)
		}
	}
}

// Complexity out of range falls back to the measured setting rather than to the library's, which
// is the setting the plan measured at three percent of a core against.
func TestComplexityFallsBackToTheMeasuredDefault(t *testing.T) {
	for _, tc := range []struct{ in, want int }{
		{0, config.DefaultXiaozhiComplexity},
		{-1, config.DefaultXiaozhiComplexity},
		{opusMaxComplexity + 1, config.DefaultXiaozhiComplexity},
		{1, 1},
		{8, 8},
	} {
		if got := complexity(tc.in); got != tc.want {
			t.Errorf("complexity(%d) = %d, want %d", tc.in, got, tc.want)
		}
	}
	if complexity(0) == opusMaxComplexity {
		t.Error("the fallback is the library default, which is the setting the plan measured against")
	}
}

// tone is one frame of a voice: loud, and loud enough for the endpoint to take it for speech.
func tone(n int) []int16 {
	out := make([]int16, n)
	for i := range out {
		out[i] = int16(8000 * math.Sin(float64(i)*0.2))
	}
	return out
}
