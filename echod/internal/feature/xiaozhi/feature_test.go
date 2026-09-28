package xiaozhi

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gorilla/websocket"

	"github.com/HuskerMinion/techo5/echod/internal/config"
)

// The whole client against a server that behaves like the cloud, over a socket a terminal can reach
// while it is all going on.
//
// This is the test that says the switch means something. The reconnect loop spends nearly all of its
// time inside a session, and a terminal that moves the switch or asks for a new token is talking to
// the daemon while it is there — so whether those commands reach a running session is not a detail.
// It is the difference between a tool that works and one that quietly waits for the cloud to hang up
// on its own, which for a ten-minute hold is ten minutes of nothing happening.
func TestClientReachesARunningSession(t *testing.T) {
	restore(t)

	// Whatever the endpoint hands out is what the WebSocket is opened with, so the test can point
	// both at one server: the token has to come from the OTA call and not from anywhere else.
	wsPath := "/xiaozhi/v1/"

	var (
		connections atomic.Int32
		authorised  = make(chan string, 8)
		open        = make(chan struct{}, 8)
		closed      = make(chan int32, 8)
	)
	up := websocket.Upgrader{}
	// The OTA answer has to name the address of the server that is answering it, and a handler
	// cannot see the server it belongs to until the server exists.
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == OTAPath {
			body, _ := json.Marshal(map[string]any{
				"websocket": map[string]string{
					"url":   "ws" + strings.TrimPrefix(srv.URL, "http") + wsPath,
					"token": "t-" + r.Header.Get("Device-Id"),
				},
			})
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write(body)
			return
		}

		conn, err := up.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		n := connections.Add(1)
		authorised <- r.Header.Get("Authorization")

		// The hello has to be read before anything can be sent, or the client's read of it is not a
		// message from the server and the rest of the test races for a reason that is not the
		// client's.
		_ = conn.SetReadDeadline(time.Now().Add(5 * time.Second))
		var hello map[string]any
		if err := conn.ReadJSON(&hello); err != nil {
			t.Errorf("session %d: no hello: %v", n, err)
		}
		if got := hello["transport"]; got != "websocket" {
			t.Errorf("hello transport = %v", got)
		}
		// The server answers with its own rate, which is the number the decoder would be built from
		// and not the one this client sends.
		if err := conn.WriteJSON(map[string]any{
			"type": TypeHello, "transport": "websocket", "session_id": fmt.Sprintf("s-%d", n),
			"audio_params": map[string]any{
				"format": "opus", "sample_rate": defaultDownlinkRate, "channels": 1, "frame_duration": 60,
			},
		}); err != nil {
			t.Errorf("session %d: no answer to the hello: %v", n, err)
		}
		open <- struct{}{}

		// The client closing is the half of this the test is here for: every command below is
		// supposed to end with this socket going away, and without this a timeout could not tell a
		// command that reached nothing from one that worked.
		_ = conn.SetReadDeadline(time.Now().Add(30 * time.Second))
		for {
			if _, _, err := conn.ReadMessage(); err != nil {
				closed <- n
				_ = conn.Close()
				return
			}
		}
	}))
	t.Cleanup(srv.Close)

	if err := config.Set().Xiaozhi().Host(srv.URL); err != nil {
		t.Fatalf("pointing the client at the test server: %v", err)
	}

	f := build()
	f.control.x = f
	ControlPath = shortSocket(t)
	ctx, cancel := context.WithCancel(context.Background())
	run := make(chan error, 1)
	go func() { run <- f.Run(ctx) }()
	t.Cleanup(func() {
		cancel()
		select {
		case err := <-run:
			if err != nil {
				t.Errorf("Run: %v", err)
			}
		case <-time.After(5 * time.Second):
			t.Error("Run did not return after its context ended")
		}
	})

	waitSocket(t, ControlPath)
	term := newTerminal(t, ControlPath)

	// Off to begin with: the switch says so, and nothing has been asked to connect.
	if _, err := term.ask(`{"cmd":"status"}`); err != nil {
		t.Fatalf("status: %v", err)
	}
	if st := f.Status(); st.State != StateOff {
		t.Fatalf("state = %q, want %q", st.State, StateOff)
	}

	// On. There is a session afterwards, carrying the rate the server said rather than the one this
	// client sends — which is the only thing the status can usefully report about the audio.
	if _, err := term.ask(`{"cmd":"on"}`); err != nil {
		t.Fatalf("on: %v", err)
	}
	waitOpen(t, open)
	waitState(t, f, StateConnected)
	if got := f.Status(); got.Session == "" || got.Downlink != defaultDownlinkRate || got.Uplink != uplinkRate {
		t.Errorf("status = %+v, want a session at %d Hz up and %d Hz down", got, uplinkRate, defaultDownlinkRate)
	}
	// The token the WebSocket was opened with is the one the OTA call handed out. If it ever comes
	// from anywhere else, the endpoint is not being asked any more and the feature is a mirage.
	if got := <-authorised; got != "Bearer t-b0:f7:c4:ff:e5:92" {
		t.Errorf("the session was opened with %q", got)
	}

	// Off, with a session up. Nothing in the loop was looking at the switch, so this is the command
	// that used to be accepted and then ignored until the cloud decided to close.
	if _, err := term.ask(`{"cmd":"off"}`); err != nil {
		t.Fatalf("off: %v", err)
	}
	waitClosed(t, closed, 1)
	waitState(t, f, StateOff)
	if _, err := f.session(); err == nil {
		t.Error("a session is still attached after the switch went off")
	}

	// On again, and this time a second connection rather than a remembered one.
	if _, err := term.ask(`{"cmd":"on"}`); err != nil {
		t.Fatalf("on again: %v", err)
	}
	waitOpen(t, open)
	waitState(t, f, StateConnected)

	// And upgrade, which is the same thing under another name: the OTA call happens on every attempt,
	// so the only way to force a new token out of the endpoint is to be in one.
	if _, err := term.ask(`{"cmd":"upgrade"}`); err != nil {
		t.Fatalf("upgrade: %v", err)
	}
	waitClosed(t, closed, 2)
	waitOpen(t, open)
	waitState(t, f, StateConnected)

	if got := connections.Load(); got != 3 {
		t.Errorf("%d sessions, want one for each of the three commands", got)
	}
	<-authorised
	<-authorised
}

// A backoff that reaches five minutes is right for a server that is saying no and useless for a
// person at a terminal. Cutting it short is the difference between the switch responding and the
// switch being something that happens eventually.
func TestPauseIsCutShort(t *testing.T) {
	f := &Feature{wake: make(chan struct{}, 1)}

	go func() {
		time.Sleep(20 * time.Millisecond)
		f.wakeMe()
	}()

	done := make(chan bool, 1)
	go func() { done <- f.pause(context.Background(), retryMax) }()

	select {
	case finished := <-done:
		if !finished {
			t.Error("pause reported the context ending, which it did not")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("a wake did not cut a five-minute wait short")
	}

	// A context that ends is a wait that is over, and pause says so rather than carrying on to the
	// end of it.
	if f.pause(cancelled(), time.Hour) {
		t.Error("pause said the wait finished when the context had ended")
	}
}

// An unactivated device is a normal state, not a fault, and the code has to reach the terminal
// standing at the device. The field name is a contract between two binaries that are deployed
// separately, so it is pinned here rather than left to whichever side was written first.
func TestActivationReachesTheTerminal(t *testing.T) {
	b, err := json.Marshal(Status{
		State: StateActivating, Host: OfficialHost, Code: "123 456", Challenge: "chal-1",
	})
	if err != nil {
		t.Fatal(err)
	}
	for k, want := range map[string]string{
		"state":           StateActivating,
		"host":            OfficialHost,
		"activation_code": "123 456",
		"challenge":       "chal-1",
	} {
		if !strings.Contains(string(b), fmt.Sprintf("%q:%q", k, want)) {
			t.Errorf("a status went out as %s, which has no %s", b, k)
		}
	}
}

// waitSocket is for Run opening its socket in another goroutine: a terminal that arrives before the
// socket is up has to be told to try again, and here that is the test waiting rather than a race.
func waitSocket(t *testing.T, path string) {
	t.Helper()
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		if c, err := net.Dial("unix", path); err == nil {
			_ = c.Close()
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("the control socket never came up at %s", path)
}

func waitOpen(t *testing.T, c chan struct{}) {
	t.Helper()
	select {
	case <-c:
	case <-time.After(15 * time.Second):
		t.Fatal("no session was opened")
	}
}

func waitClosed(t *testing.T, c chan int32, session int32) {
	t.Helper()
	select {
	case n := <-c:
		if n != session {
			t.Errorf("session %d closed, want %d", n, session)
		}
	case <-time.After(15 * time.Second):
		t.Fatalf("session %d was not closed, so the command reached nothing", session)
	}
}

func waitState(t *testing.T, f *Feature, want string) {
	t.Helper()
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		if st := f.Status(); st.State == want {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("state = %q, want %q", f.Status().State, want)
}

func cancelled() context.Context {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	return ctx
}
