package sendspin

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gorilla/websocket"

	"github.com/HuskerMinion/techo5/echod/internal/config"
)

func TestMusicAssistantSendspinAddress(t *testing.T) {
	for api, want := range map[string]string{
		"http://100.64.0.10:8095":     "ws://100.64.0.10:8927/sendspin",
		"https://ma.example.com/":     "wss://ma.example.com:8927/sendspin",
		"http://[fd7a::1]:8095":       "ws://[fd7a::1]:8927/sendspin",
		"":                            "",
		"ftp://192.168.1.20":          "",
		"not a url at all, really ::": "",
	} {
		if got := maSendspinURL(api); got != want {
			t.Errorf("%q: %q, want %q", api, got, want)
		}
	}
}

// fakeMA is a Music Assistant Sendspin server that counts the devices that dial it.
func fakeMA(t *testing.T) (port int, dialed *atomic.Int32) {
	dialed = &atomic.Int32{}
	up := websocket.Upgrader{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != path {
			http.NotFound(w, r)
			return
		}
		// Counted as the request arrives: counted after the upgrade, the device can see its session
		// start before the count does (a flake seen in CI).
		dialed.Add(1)
		c, err := up.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		c.ReadMessage() // held until the device hangs up
		c.Close()
	}))
	t.Cleanup(srv.Close)
	_, p, _ := net.SplitHostPort(srv.Listener.Addr().String())
	port, _ = strconv.Atoi(p)
	return port, dialed
}

// The device dials Music Assistant when it is set up, and only then, and not while a server that
// dialed in holds the room.
func TestTheDeviceDialsMusicAssistant(t *testing.T) {
	config.Use(filepath.Join(t.TempDir(), "state.json"))
	port, dialed := fakeMA(t)
	wasPort, wasRun := maPort, runSession
	maPort = port
	ran := make(chan struct{}, 4)
	runSession = func(ctx context.Context, l *listener, conn *websocket.Conn, name string) error {
		ran <- struct{}{}
		<-ctx.Done()
		return nil
	}
	t.Cleanup(func() { maPort, runSession = wasPort, wasRun })

	l := newListener(nil, nil, func(string) {})

	// Not set up: nothing is dialed.
	ctx, cancel := context.WithCancel(context.Background())
	go l.dial(ctx, "Test")
	time.Sleep(300 * time.Millisecond)
	cancel()
	if dialed.Load() != 0 {
		t.Fatal("dialed with no Music Assistant set up")
	}

	// A server already in the room: nothing is dialed.
	tok := "t"
	if err := config.Set().MusicAssistant().Server("http://127.0.0.1:8095", &tok); err != nil {
		t.Fatal(err)
	}
	l.take()
	ctx, cancel = context.WithCancel(context.Background())
	go l.dial(ctx, "Test")
	time.Sleep(300 * time.Millisecond)
	cancel()
	if dialed.Load() != 0 {
		t.Fatal("dialed while a server held the room")
	}
	l.give()

	// Set up and free: dialed, and the session runs on it.
	ctx, cancel = context.WithCancel(context.Background())
	defer cancel()
	go l.dial(ctx, "Test")
	select {
	case <-ran:
	case <-time.After(5 * time.Second):
		t.Fatal("the device never dialed Music Assistant")
	}
	if dialed.Load() != 1 {
		t.Errorf("dialed %d times", dialed.Load())
	}
	if l.take() {
		t.Error("the room was free while the dialed session ran")
	}
}
