package xiaozhi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

// A whole conversation against a server that behaves like the cloud: it checks the headers, reads
// the hello, answers with its own, and then says several things — some of which this client has no
// business refusing to understand.
func TestSessionSpeaksTheProtocol(t *testing.T) {
	const sessionID = "s-1234"

	var (
		clientHello map[string]any
		headers     http.Header
		// The server half of the conversation runs in another goroutine, so its complaints have to
		// cross back through a channel rather than a variable, or the race detector holds the whole
		// test to account.
		wsErr = make(chan error, 8)
	)

	up := websocket.Upgrader{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		headers = r.Header.Clone()
		conn, err := up.Upgrade(w, r, nil)
		if err != nil {
			wsErr <- err
			return
		}
		defer func() { _ = conn.Close() }()

		if err := conn.ReadJSON(&clientHello); err != nil {
			wsErr <- err
			return
		}
		if err := conn.WriteJSON(map[string]any{
			"type": TypeHello, "transport": "websocket", "session_id": sessionID,
			"audio_params": map[string]any{
				"format": "opus", "sample_rate": defaultDownlinkRate, "channels": 1, "frame_duration": 60,
			},
		}); err != nil {
			wsErr <- err
			return
		}

		// A ping, an unreadable line, something this client has never heard of, and a transcript.
		for _, msg := range []any{
			map[string]any{"type": TypePing},
			[]byte("{not json"),
			map[string]any{"type": "iq", "request_id": 3},
			map[string]any{"type": TypeSTT, "text": "今天天气怎么样"},
		} {
			if err := conn.WriteMessage(websocket.TextMessage, mustJSON(t, msg)); err != nil {
				wsErr <- err
				return
			}
		}

		// Downlink audio, counted but not decoded: there is no decoder until M3.
		if err := conn.WriteMessage(websocket.BinaryMessage, make([]byte, 180)); err != nil {
			wsErr <- err
			return
		}

		// The pong is the client's answer to the ping, and it has to come before the close.
		_ = conn.SetReadDeadline(time.Now().Add(5 * time.Second))
		var pong map[string]any
		if err := conn.ReadJSON(&pong); err != nil {
			wsErr <- err
			return
		}
		if pong["type"] != TypePong || pong["session_id"] != sessionID {
			wsErr <- errors.New("the client did not answer the ping with a pong naming the session")
			return
		}

		if err := conn.WriteControl(websocket.CloseMessage,
			websocket.FormatCloseMessage(CloseNotActivated, ""), time.Now().Add(time.Second)); err != nil {
			wsErr <- err
		}
	}))
	defer srv.Close()

	id := identity{DeviceID: "b0:f7:c4:ff:e5:92", ClientID: "6b2f7e9c-3d1a-4c58-9f0e-2a1b3c4d5e6f"}
	sess, err := dial(context.Background(), "ws"+strings.TrimPrefix(srv.URL, "http"), "test-token", id)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer func() { _ = sess.Close() }()

	if got := headers.Get("Authorization"); got != "Bearer test-token" {
		t.Errorf("Authorization = %q", got)
	}
	for k, want := range map[string]string{
		"Protocol-Version": "1",
		"Device-Id":        id.DeviceID,
		"Client-Id":        id.ClientID,
	} {
		if got := headers.Get(k); got != want {
			t.Errorf("%s = %q, want %q", k, got, want)
		}
	}

	// The two rates are not the same number, and the one the server said is the one the decoder has
	// to be built from.
	if sess.Rate() != defaultDownlinkRate {
		t.Errorf("Rate() = %d, want the server's %d, not our own %d", sess.Rate(), defaultDownlinkRate, uplinkRate)
	}
	if sess.ID() != sessionID {
		t.Errorf("ID() = %q, want %q", sess.ID(), sessionID)
	}

	// The uplink is what the microphones already produce, and this client claims no extensions: MCP
	// is tools the cloud calls into the device, and there is no reason to open that on a Show in
	// somebody's house.
	if got := clientHello["audio_params"].(map[string]any); got["sample_rate"] != float64(uplinkRate) {
		t.Errorf("uplink %v, want %d Hz", got["sample_rate"], uplinkRate)
	}
	features, _ := clientHello["features"].(map[string]any)
	if len(features) != 0 {
		t.Errorf("features = %v, want none advertised", features)
	}

	events := make(chan Event, 16)
	err = sess.Serve(context.Background(), func(e Event) { events <- e })
	if err == nil {
		t.Error("Serve returned nil after the server closed with 1002")
	} else if !strings.Contains(err.Error(), "activated") {
		t.Errorf("error %q does not say the device is not activated, which is what 1002 means here", err)
	}
	// The handler is one step behind: it wrote the close frame that ended Serve, and has to finish
	// sending its own complaints after that. A second is nothing, and a handler that has not said
	// anything by then has nothing to say.
	select {
	case err := <-wsErr:
		t.Errorf("the server side: %v", err)
	case <-time.After(time.Second):
	}

	var stt, unknown, audio string
	for range 4 {
		select {
		case e := <-events:
			switch e.Type {
			case TypeSTT:
				stt = e.Text
			case "iq":
				unknown = e.Type
			case TypeTTS:
				audio = "yes"
			}
		case <-time.After(2 * time.Second):
			t.Fatal("the client stopped delivering events")
		}
	}
	if stt != "今天天气怎么样" {
		t.Errorf("transcript = %q, want the stt text", stt)
	}
	if unknown != "iq" {
		t.Error("a message this client has never heard of was dropped instead of passed on")
	}
	if audio != "yes" {
		t.Error("the downlink audio was not reported")
	}

	st := sess.Stats()
	// Four text frames including the one that would not parse: it arrived, and a diagnostic that
	// hides it hides the reason to look.
	if st.Messages != 5 || st.Text != 4 || st.Audio != 1 || st.Bytes != 180 || st.Pings != 1 {
		t.Errorf("stats = %+v, want the counts of everything the server sent", st)
	}
	if time.Since(st.Since) < 0 {
		t.Errorf("Since = %v", st.Since)
	}
}

// The decoder is built from the server's answer. A server that leaves the rate out gets the one the
// protocol documents, which is 24 kHz and not the 16 kHz this client sends.
func TestDownlinkRate(t *testing.T) {
	for _, tc := range []struct {
		name string
		in   *AudioParams
		want int
	}{
		{"said", &AudioParams{SampleRate: 16000}, 16000},
		{"left out", nil, defaultDownlinkRate},
		{"said zero", &AudioParams{SampleRate: 0}, defaultDownlinkRate},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := downlinkRate(tc.in); got != tc.want {
				t.Errorf("downlinkRate(%+v) = %d, want %d", tc.in, got, tc.want)
			}
		})
	}
	if uplinkRate == defaultDownlinkRate {
		t.Error("the two rates are the same, so the trap this guards against cannot happen and the test is not testing anything")
	}
}

// 1002 gets its own sentence. An unactivated device gets a clean upgrade and a completed hello and
// is then closed, which from inside a WebSocket library looks like a fault and is not one.
func TestReadErrorExplainsTheClose(t *testing.T) {
	for _, tc := range []struct {
		name  string
		err   error
		want  string
		plain string
	}{
		{
			name:  "not activated",
			err:   &websocket.CloseError{Code: CloseNotActivated},
			plain: "not activated",
		},
		{
			name:  "going away",
			err:   &websocket.CloseError{Code: 1001, Text: "going away"},
			plain: "1001",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := readError(tc.err).Error()
			if !strings.Contains(got, tc.plain) {
				t.Errorf("readError = %q, want it to mention %q", got, tc.plain)
			}
			if strings.Contains(got, websocket.ErrCloseSent.Error()) {
				t.Errorf("readError = %q, want the close code rather than the transport's wording", got)
			}
		})
	}
}

func mustJSON(t *testing.T, v any) []byte {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshalling %v: %v", v, err)
	}
	return b
}
