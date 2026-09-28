package xiaozhi

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"net"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

// A terminal reaches a running client without the daemon stopping, which is the whole reason this
// socket exists: every other tool in this binary has to take the microphones or the speaker first,
// and the client wants neither to be asked a question.
func TestControlAnswersWithoutASession(t *testing.T) {
	ctl, term := listening(t)
	defer ctl.stop()

	r, err := term.ask(`{"cmd":"status"}`)
	if err != nil {
		t.Fatalf("status: %v", err)
	}
	if r.Status == nil {
		t.Fatalf("status answered %+v", r)
	}
	if r.Status.State != StateOff {
		t.Errorf("state = %q, want %q: the client is off until somebody turns it on", r.Status.State, StateOff)
	}
}

// The switch is the same one Home Assistant moves, and a terminal moving it has to be the same
// change: the setting on disk is what survives a reboot and the entity is what shows it.
func TestControlMovesTheSwitch(t *testing.T) {
	ctl, term := listening(t)
	defer ctl.stop()

	if _, err := term.ask(`{"cmd":"on"}`); err != nil {
		t.Fatalf("on: %v", err)
	}
	if !ctl.x.enabled.Get() {
		t.Error("the entity still shows off")
	}
	if !configEnabled(t) {
		t.Error("the setting was not written, so a reboot would lose it")
	}

	if _, err := term.ask(`{"cmd":"off"}`); err != nil {
		t.Fatalf("off: %v", err)
	}
	if ctl.x.enabled.Get() {
		t.Error("the entity still shows on")
	}
	if configEnabled(t) {
		t.Error("the setting was not written")
	}
}

// A listen with no session behind it has to say so, and say what state the client is in, rather than
// silently doing nothing: a terminal watching an empty answer cannot tell a broken socket from a
// client that has not connected yet.
func TestControlRefusesWithoutASession(t *testing.T) {
	_, term := listening(t)

	for _, line := range []string{`{"cmd":"listen","state":"start"}`, `{"cmd":"abort"}`} {
		if err := term.write(line); err != nil {
			t.Fatalf("%s: %v", line, err)
		}
		r, err := term.next()
		if err != nil {
			t.Fatalf("%s: %v", line, err)
		}
		if r.Event != eventError || r.Err == "" {
			t.Errorf("%s answered %+v, want an error that says why", line, r)
		}
	}
}

// A terminal is a person typing, not a machine, so a line it got wrong is answered rather than
// dropped: a socket that closes on a typo is a socket somebody gives up on.
func TestControlSurvivesNonsense(t *testing.T) {
	_, term := listening(t)

	for _, line := range []string{`{`, `[]`, `{"cmd":"what"}`, ``, `null`} {
		if err := term.write(line); err != nil {
			t.Fatalf("%q: %v", line, err)
		}
		r, err := term.next()
		if err != nil {
			t.Fatalf("%q: %v", line, err)
		}
		if r.Event != eventError {
			t.Errorf("%q answered %+v, want an error", line, r)
		}
	}

	// And it still works afterwards, which is the part that matters: one bad line must not take the
	// connection with it.
	r, err := term.ask(`{"cmd":"status"}`)
	if err != nil {
		t.Fatalf("after four bad lines: %v", err)
	}
	if r.Event != eventStatus {
		t.Errorf("after four bad lines the socket answered %+v", r)
	}
}

// A terminal that has gone away without closing — a dropped ssh, a killed process — must not hold up
// the terminal that is still there. The write is bounded, and a write that runs out of time drops the
// subscriber.
func TestControlDropsATerminalThatStoppedReading(t *testing.T) {
	ctl, term := listening(t)
	defer ctl.stop()

	stuck := dialControl(t)
	// Fill the socket so the next write has nowhere to go. A unix socket's buffer is small and 64 kB
	// is well past it.
	for range 64 {
		if _, err := stuck.Write(make([]byte, 1024)); err != nil {
			break
		}
	}

	done := make(chan struct{})
	go func() {
		defer close(done)
		for range 3 {
			ctl.x.set(Status{State: StateConnected, Session: "s-1"})
		}
	}()
	select {
	case <-done:
	case <-time.After(3 * controlWrite):
		t.Fatal("a terminal that stopped reading held up the status everyone else was sent")
	}

	// The terminal that is still reading is unaffected.
	if r, err := term.ask(`{"cmd":"status"}`); err != nil {
		t.Fatalf("the other terminal: %v", err)
	} else if r.Status.Session != "s-1" {
		t.Errorf("status = %+v, want the broadcasts to have got through", r.Status)
	}
}

// The socket is left behind by a process that was killed rather than stopped, and net.Listen on an
// existing path is an error rather than a reconnect. So a stale one is cleared, or a crash is how a
// client stops working for good.
func TestControlClearsAStaleSocket(t *testing.T) {
	restore(t)
	path := shortSocket(t)
	old := ControlPath
	ControlPath = path
	t.Cleanup(func() { ControlPath = old })

	if err := os.WriteFile(path, nil, 0o600); err != nil {
		t.Fatalf("leaving a stale socket: %v", err)
	}

	f := &Feature{wake: make(chan struct{}, 1)}
	f.control = newControl(f)
	f.control.x = f
	if err := f.control.serve(context.Background()); err != nil {
		t.Fatalf("serve over a stale socket: %v", err)
	}
	defer f.control.stop()

	if _, err := net.Dial("unix", path); err != nil {
		t.Errorf("the new socket does not accept: %v", err)
	}
}

// stopping takes the socket away with it, so a restart does not find a file that answers nothing.
func TestControlStopRemovesTheSocket(t *testing.T) {
	ctl, _ := listening(t)

	ctl.stop()
	if _, err := os.Stat(ControlPath); !os.IsNotExist(err) {
		t.Errorf("%s is still there after stop: %v", ControlPath, err)
	}
	ctl.stop() // twice, because Run's defer and the context both reach it
}

// listening puts a control socket somewhere harmless and gives back the feature behind it and one
// terminal connected to it.
func listening(t *testing.T) (*Control, *terminal) {
	t.Helper()

	restore(t)
	path := shortSocket(t)
	old := ControlPath
	ControlPath = path
	t.Cleanup(func() { ControlPath = old })

	f := build()
	f.control.x = f
	if err := f.control.serve(context.Background()); err != nil {
		t.Fatalf("serve: %v", err)
	}
	t.Cleanup(f.control.stop)

	if _, err := os.Stat(path); err != nil {
		t.Fatalf("no socket at %s: %v", path, err)
	}
	return f.control, newTerminal(t, path)
}

// shortSocket is a socket path short enough for every platform this is tested on.
//
// A unix socket path is capped at about a hundred characters and Go's own t.TempDir sits under a
// long per-test directory on a Mac, so a test that used it would fail for a reason that has nothing
// to do with the socket. The device's own path is /data/misc/techo5/xiaozhi.sock.
func shortSocket(t *testing.T) string {
	t.Helper()
	dir, err := os.MkdirTemp("/tmp", "xz")
	if err != nil {
		t.Fatalf("a temporary directory: %v", err)
	}
	if real, err := filepath.EvalSymlinks(dir); err == nil {
		dir = real
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	return filepath.Join(dir, "c.sock")
}

func dialControl(t *testing.T) net.Conn {
	t.Helper()
	conn, err := net.Dial("unix", ControlPath)
	if err != nil {
		t.Fatalf("dialing %s: %v", ControlPath, err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	return conn
}

// terminal is one connection, with the reader that has to go with it.
//
// One bufio.Reader for the life of the connection, because a second would start wherever the first
// had already read ahead to and everything in between would be gone.
type terminal struct {
	conn net.Conn
	read *bufio.Reader
	mu   sync.Mutex
}

func newTerminal(t *testing.T, path string) *terminal {
	t.Helper()
	conn, err := net.Dial("unix", path)
	if err != nil {
		t.Fatalf("dialing %s: %v", path, err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	return &terminal{conn: conn, read: bufio.NewReader(conn)}
}

// ask sends one line and reads until the reply that ends it, which is the first status or the first
// error; anything else in between is an event and belongs to a watch. A refusal comes back as an
// error, so a happy-path caller cannot quietly ignore one.
func (x *terminal) ask(line string) (reply, error) {
	if err := x.write(line); err != nil {
		return reply{}, err
	}
	for {
		r, err := x.next()
		if err != nil {
			return reply{}, err
		}
		switch r.Event {
		case eventError:
			return r, errors.New(r.Err)
		case eventStatus, eventGoodbye:
			return r, nil
		}
	}
}

func (x *terminal) write(line string) error {
	_, err := x.conn.Write([]byte(line + "\n"))
	return err
}

func (x *terminal) next() (reply, error) {
	x.mu.Lock()
	defer x.mu.Unlock()

	_ = x.conn.SetReadDeadline(time.Now().Add(10 * time.Second))
	line, err := x.read.ReadString('\n')
	if err != nil {
		return reply{}, err
	}
	var r reply
	if err := json.Unmarshal([]byte(line), &r); err != nil {
		return reply{}, err
	}
	return r, nil
}
