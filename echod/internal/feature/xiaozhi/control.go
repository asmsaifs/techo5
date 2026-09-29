package xiaozhi

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/HuskerMinion/techo5/echod/internal/config"
	"github.com/HuskerMinion/techo5/echod/internal/layout"
	"github.com/HuskerMinion/techo5/echod/internal/lib/safe"
)

// The control socket is how a terminal reaches a running session.
//
// Every other tool in this binary has to stop the daemon first — the microphone, the speaker and the
// playback device are held exclusively, and the tools that want them kill the service and start it
// again afterwards. This one wants none of that hardware, so it does not have to: it is a unix
// socket under the state directory, and `echod tools xiaozhi` drives the same client the switch
// does, whether the switch is on or not and whether a session is up or down.
//
// Newline-delimited JSON in both directions, because it is a debugging surface and not a protocol:
// one line per message, readable with nothing on the device but a terminal.

// ControlPath is where the socket is. A variable so a test can put it somewhere harmless.
var ControlPath = layout.StateDir + "/xiaozhi.sock"

// errClientID is what the OTA endpoint answers an uncoloured client id with: a bare 400, with a body
// naming neither field. Saying it here turns an hour of guessing into a sentence.
var errClientID = errors.New("the xiaozhi client id has to be a UUID with its dashes")

// The events a subscriber is sent.
const (
	eventStatus  = "status"
	eventMessage = "message"
	eventTurn    = "turn"
	eventError   = "error"
	eventGoodbye = "goodbye"
)

// The commands a terminal may send.
const (
	cmdStatus  = "status"
	cmdOn      = "on"
	cmdOff     = "off"
	cmdListen  = "listen"
	cmdAbort   = "abort"
	cmdUpgrade = "upgrade"
)

// controlWrite is how long a subscriber gets to take a line. A terminal that has gone away without
// closing — a dropped ssh, a killed process — would otherwise block the goroutine writing to it for
// as long as the socket buffer lasts, and then leak it.
const controlWrite = 2 * time.Second

// request is one line from a terminal.
type request struct {
	Cmd string `json:"cmd"`

	// State and Mode are for listen, Reason for abort.
	State  string `json:"state,omitempty"`
	Mode   string `json:"mode,omitempty"`
	Reason string `json:"reason,omitempty"`
}

// reply is one line back. Everything is answered before the stream opens, so a terminal that asked a
// question gets its answer even if nothing has happened since.
type reply struct {
	Event  string  `json:"event"`
	Status *Status `json:"status,omitempty"`
	Msg    *Event  `json:"msg,omitempty"`
	Turn   *Turn   `json:"turn,omitempty"`
	Err    string  `json:"err,omitempty"`
}

// Control is the socket, and the set of terminals watching it.
type Control struct {
	x *Feature

	mu sync.Mutex
	ln net.Listener

	// path is where ln is, kept so stopping can take the socket away without reading the package
	// variable from another goroutine.
	path string

	subs   map[*subscriber]struct{}
	closed bool
}

// newControl is the socket, not yet listening.
func newControl(x *Feature) *Control {
	return &Control{x: x, subs: map[*subscriber]struct{}{}}
}

// serve opens the socket and takes connections until the context ends.
//
// A stale file is removed first. It is the one thing that survives the process that made it: a
// daemon killed hard leaves the socket behind, and net.Listen on an existing path is an error rather
// than a reconnect, which is how a crash would turn into a client that never works again.
func (c *Control) serve(ctx context.Context) error {
	if err := os.MkdirAll(filepath.Dir(ControlPath), 0o700); err != nil {
		return fmt.Errorf("xiaozhi control: %w", err)
	}
	if err := os.Remove(ControlPath); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("xiaozhi control: clearing %s: %w", ControlPath, err)
	}

	ln, err := net.Listen("unix", ControlPath)
	if err != nil {
		return fmt.Errorf("xiaozhi control: listening on %s: %w", ControlPath, err)
	}
	// The state directory is root-only already; this says so on the socket too, and costs nothing.
	if err := os.Chmod(ControlPath, 0o600); err != nil {
		slog.Warn("xiaozhi: the control socket could not be made private", "err", err)
	}

	c.mu.Lock()
	c.ln, c.path = ln, ControlPath
	c.mu.Unlock()

	safe.Go("xiaozhi control accept", func() { c.accept(ln) })
	safe.Go("xiaozhi control", func() {
		<-ctx.Done()
		c.stop()
	})
	slog.Info("xiaozhi: control socket is up", "path", ControlPath)
	return nil
}

func (c *Control) stop() {
	c.mu.Lock()
	ln, path := c.ln, c.path
	c.ln, c.path, c.closed = nil, "", true
	subs := make([]*subscriber, 0, len(c.subs))
	for s := range c.subs {
		subs = append(subs, s)
	}
	c.mu.Unlock()

	if ln != nil {
		_ = ln.Close()
	}
	for _, s := range subs {
		s.close()
	}
	// The path it was given rather than the one named now: ControlPath is a package variable, and
	// reading it here means taking it down from another goroutine than the one that set it. It is
	// the same path in echod and only the same path by accident in a test.
	_ = os.Remove(path)
}

// broadcast tells every terminal watching what just happened. A terminal that is not there any more
// is dropped rather than allowed to hold up the one that is.
func (c *Control) broadcast(r reply) {
	c.mu.Lock()
	subs := make([]*subscriber, 0, len(c.subs))
	for s := range c.subs {
		subs = append(subs, s)
	}
	c.mu.Unlock()

	for _, s := range subs {
		if !s.send(r) {
			c.drop(s)
		}
	}
}

func (c *Control) drop(s *subscriber) {
	c.mu.Lock()
	delete(c.subs, s)
	c.mu.Unlock()
	s.close()
}

func (c *Control) add(s *subscriber) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		return
	}
	c.subs[s] = struct{}{}
}

// accept takes connections until the listener is closed.
func (c *Control) accept(ln net.Listener) {
	for {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		sub := &subscriber{conn: conn, c: c}
		c.add(sub)
		safe.Go("xiaozhi control "+conn.RemoteAddr().String(), func() { sub.serve() })
	}
}

// subscriber is one connected terminal.
type subscriber struct {
	conn net.Conn
	c    *Control

	mu sync.Mutex
}

func (s *subscriber) serve() {
	defer func() {
		s.c.drop(s)
		s.close()
	}()

	lines := bufio.NewScanner(s.conn)
	for lines.Scan() {
		var req request
		line := lines.Bytes()
		if err := json.Unmarshal(line, &req); err != nil {
			s.send(reply{Event: eventError, Err: "not a command: " + err.Error()})
			continue
		}
		// Only when there is something to say: opening a turn answers itself, as a broadcast, so
		// that a terminal watching rather than asking is told about it too. Answering it here as
		// well would put two goodbyes on the wire and leave the reader to guess which is the
		// answer to what it asked.
		if r, ok := s.c.answer(req); ok && !s.send(r) {
			return
		}
	}
}

// answer does what was asked, and says what came of it. The second return is whether there is
// anything left to say.
func (c *Control) answer(req request) (reply, bool) {
	switch req.Cmd {
	case cmdStatus:
		st := c.x.Status()
		return reply{Event: eventStatus, Status: &st}, true

	case cmdOn, cmdOff:
		on := req.Cmd == cmdOn
		if err := config.Set().Xiaozhi().Enabled(on); err != nil {
			return reply{Event: eventError, Err: err.Error()}, true
		}
		c.x.enabled.Set(on)
		slog.Info("xiaozhi: switched from a terminal", "enabled", on)
		// interrupt, not a nudge: the client is most likely inside a session, and that is the thing
		// that has to go for the switch to mean anything.
		c.x.interrupt()
		st := c.x.Status()
		return reply{Event: eventStatus, Status: &st}, true

	case cmdListen:
		if req.State == ListenStop {
			// A stop is a stop whatever is open, so it is never an error: the device sends one from
			// a button without knowing what the client is doing, and a refusal there would be the
			// first thing anybody saw of it.
			c.x.Stop()
			slog.Info("xiaozhi: listen stopped from a terminal")
			return reply{Event: eventGoodbye}, true
		}
		if req.State != "" && req.State != ListenStart {
			return reply{Event: eventError, Err: "listen takes start or stop"}, true
		}
		// The turn opens itself, and sends the listen start as part of that. A terminal asking for
		// one is answered the moment it is running, and is told about the end of it afterwards as
		// a turn event — which is the only way to see a turn the device ended by itself.
		if err := c.x.Listen(req.Mode); err != nil {
			return reply{Event: eventError, Err: err.Error()}, true
		}
		return reply{}, false // the turn opening is its own answer

	case cmdAbort:
		sess, err := c.x.session()
		if err != nil {
			return reply{Event: eventError, Err: err.Error()}, true
		}
		reason := req.Reason
		if reason == "" {
			reason = AbortWakeWord
		}
		// The uplink first. A turn that is cut short and then goes on sending is a turn the server
		// has to work out how to end, and one it has already been told has ended.
		c.x.Stop()
		if err := sess.Abort(reason); err != nil {
			return reply{Event: eventError, Err: err.Error()}, true
		}
		slog.Info("xiaozhi: abort from a terminal", "reason", reason)
		return reply{Event: eventGoodbye}, true

	case cmdUpgrade:
		// The OTA call happens on every connection attempt, so asking for a new one is asking to
		// reconnect — which is also the only way to force a new token out of the endpoint, and the
		// only way it happens at all while a session is up.
		slog.Info("xiaozhi: reconnecting on request")
		c.x.interrupt()
		return reply{Event: eventGoodbye}, true
	}
	return reply{Event: eventError, Err: "no such command: " + req.Cmd}, true
}

// send writes one line, and reports whether the terminal is still there.
func (s *subscriber) send(r reply) bool {
	b, err := json.Marshal(r)
	if err != nil {
		slog.Warn("xiaozhi: a control reply would not encode", "err", err)
		return true
	}
	b = append(b, '\n')

	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.conn.SetWriteDeadline(time.Now().Add(controlWrite)); err != nil {
		return false
	}
	_, err = s.conn.Write(b)
	return err == nil
}

func (s *subscriber) close() {
	_ = s.conn.Close()
}
