package echod

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/spf13/cobra"

	"github.com/HuskerMinion/techo5/echod/internal/feature/xiaozhi"
	"github.com/HuskerMinion/techo5/echod/internal/layout"
)

// Every other tool here has to stop the daemon first, because the hardware they want is held
// exclusively. This one does not: it talks to the running client over a unix socket, so a session can
// be watched and a listen can be sent without touching the microphones or the speaker, and without
// init restarting the service underneath.
func newXiaozhiCmd() *cobra.Command {
	var (
		once   bool
		reason string
		mode   string
		state  string
	)

	c := &cobra.Command{
		Use:   "xiaozhi [status|watch|on|off|listen|abort|upgrade]",
		Short: "Drive the xiaozhi voice backend in the running daemon",
		Long: "The second voice backend, on its own protocol: a WebSocket of JSON and Opus to a\n" +
			"service that does its own recognition, agent and speech. It shares the microphone\n" +
			"and the speaker with the Home Assistant pipeline and is switched separately.\n\n" +
			"With no argument, reports the state and leaves. `watch` keeps printing what the\n" +
			"server says — transcripts, replies, speech states, activations — until interrupted,\n" +
			"which is how a ten-minute hold is checked without a screen.\n\n" +
			"The daemon has to be running, and it answers on a unix socket in its state\n" +
			"directory:\n  /data/misc/techo5/xiaozhi.sock\n\n" +
			"An unactivated device says so here, with the code to type at xiaozhi.me. The upgrade\n" +
			"is not the gate: the cloud completes the WebSocket handshake with an unactivated\n" +
			"device and closes it a moment later, so the code is what decides.",
		Args:      cobra.MaximumNArgs(1),
		ValidArgs: []string{"status", "watch", "on", "off", "listen", "abort", "upgrade"},
		RunE: func(cmd *cobra.Command, args []string) error {
			verb := "status"
			if len(args) == 1 {
				verb = args[0]
			}

			ctl, err := dialControl()
			if err != nil {
				return err
			}
			defer func() { _ = ctl.Close() }()

			ctx, stop := signal.NotifyContext(cmd.Context(), os.Interrupt, syscall.SIGTERM)
			defer stop()

			switch verb {
			case "status":
				return ctl.ask(cmd, request{Cmd: "status"})
			case "watch":
				return ctl.watch(ctx, cmd)
			case "on", "off", "upgrade":
				return ctl.ask(cmd, request{Cmd: verb})
			case "listen":
				if once {
					// A bare `listen` would leave the microphone open until somebody said stop, and on
					// a device two backends share that is the wrong default. Asking for a turn rather
					// than a state is what a person means by it.
					return ctl.ask(cmd, request{Cmd: "listen", State: "start", Mode: mode})
				}
				if state != "start" && state != "stop" {
					return fmt.Errorf("--state takes start or stop; got %q", state)
				}
				return ctl.ask(cmd, request{Cmd: "listen", State: state, Mode: mode})
			case "abort":
				return ctl.ask(cmd, request{Cmd: "abort", Reason: reason})
			}
			return fmt.Errorf("expected status, watch, on, off, listen, abort or upgrade; got %q", verb)
		},
	}

	c.Flags().StringVar(&state, "state", "start", "for listen: start or stop")
	c.Flags().BoolVar(&once, "once", false, "for listen: one turn, rather than holding the microphone open")
	c.Flags().StringVar(&mode, "mode", "auto", "for listen: auto leaves the end of an utterance to the server")
	c.Flags().StringVar(&reason, "reason", "wake_word_detected", "for abort: why the turn was cut short")
	return c
}

// The wire types, mirrored from the feature rather than imported.
//
// This binary has to be able to talk to a daemon that is older than it, and a socket whose shape it
// cannot read is a socket it can still be told does not answer. A field added on the far side shows
// up here as absent, which is the failure that should be survivable.
type request struct {
	Cmd    string `json:"cmd"`
	State  string `json:"state,omitempty"`
	Mode   string `json:"mode,omitempty"`
	Reason string `json:"reason,omitempty"`
}

type status struct {
	State    string `json:"state"`
	Detail   string `json:"detail,omitempty"`
	Host     string `json:"host,omitempty"`
	URL      string `json:"url,omitempty"`
	Code     string `json:"activation_code,omitempty"`
	Session  string `json:"session,omitempty"`
	Uplink   int    `json:"uplink_hz"`
	Downlink int    `json:"downlink_hz"`
	Since    string `json:"since"`
	Messages int    `json:"stats.messages"`
	Text     int    `json:"stats.text"`
	Audio    int    `json:"stats.audio"`
	Bytes    int    `json:"stats.bytes"`
	Pings    int    `json:"stats.pings"`
}

type reply struct {
	Event  string          `json:"event"`
	Status *status         `json:"status"`
	Msg    json.RawMessage `json:"msg"`
	Err    string          `json:"err"`
}

// control is the socket and the one reader over it.
//
// One scanner for the whole life of the connection, because two would not do: bufio reads ahead, so
// a second scanner would start somewhere past whatever the first one had already buffered, and every
// line in between would be gone.
type control struct {
	conn net.Conn
	read *bufio.Scanner

	// closed is closed on the way out so that a read blocked in Scan returns instead of waiting for
	// a line that a client with nothing to say will not send for minutes.
	closed  chan struct{}
	closing sync.Once
}

func dialControl() (*control, error) {
	if _, err := os.Stat(xiaozhi.ControlPath); err != nil {
		return nil, fmt.Errorf("%s: %w\nis the daemon running? `setprop ctl.stop %s`, then start it again",
			xiaozhi.ControlPath, err, layout.ServiceName)
	}
	conn, err := net.DialTimeout("unix", xiaozhi.ControlPath, 5*time.Second)
	if err != nil {
		return nil, fmt.Errorf("connecting to %s: %w", xiaozhi.ControlPath, err)
	}
	return &control{conn: conn, read: bufio.NewScanner(conn), closed: make(chan struct{})}, nil
}

// Close is safe to call from another goroutine, which Close alone is not: the read is parked in a
// syscall and only the connection can unblock it.
func (c *control) Close() error {
	var err error
	c.closing.Do(func() {
		close(c.closed)
		err = c.conn.Close()
	})
	return err
}

// send writes one command line.
func (c *control) send(req request) error {
	b, err := json.Marshal(req)
	if err != nil {
		return err
	}
	if _, err := c.conn.Write(append(b, '\n')); err != nil {
		return fmt.Errorf("sending %s: %w", req.Cmd, err)
	}
	return nil
}

// next reads one line. The stream is endless, so this is one event rather than the end of a
// conversation: the caller decides what an event ends a command with.
func (c *control) next() (reply, error) {
	if c.read.Scan() {
		var r reply
		if err := json.Unmarshal(c.read.Bytes(), &r); err != nil {
			return reply{}, fmt.Errorf("a line from the daemon is not readable: %w", err)
		}
		return r, nil
	}
	if err := c.read.Err(); err != nil {
		return reply{}, err
	}
	return reply{}, errors.New("the daemon closed the connection")
}

// ask sends one command, prints the answer and leaves.
func (c *control) ask(cmd *cobra.Command, req request) error {
	if err := c.send(req); err != nil {
		return err
	}
	for {
		r, err := c.next()
		if err != nil {
			return err
		}
		switch r.Event {
		case "error":
			return errors.New(r.Err)
		case "status":
			return show(cmd, r.Status)
		case "goodbye":
			fmt.Fprintf(cmd.OutOrStdout(), "%s: ok\n", req.Cmd)
			return nil
		}
	}
}

// watch prints everything until interrupted, starting with the state as it is now.
//
// The interrupt is the point: this is the command a ten-minute hold is checked with, and a client
// that is holding a session open and saying nothing is exactly the one a watcher wants to stop
// watching. So the signal closes the connection, which is the only thing that unblocks a read
// parked in Scan — cancelling a context does not, and the line that would end the wait may be ten
// minutes away.
func (c *control) watch(ctx context.Context, cmd *cobra.Command) error {
	if err := c.send(request{Cmd: "status"}); err != nil {
		return err
	}
	stopped := make(chan struct{})
	defer close(stopped)
	go func() {
		select {
		case <-ctx.Done():
			_ = c.Close()
		case <-stopped:
		}
	}()

	first := true
	for {
		r, err := c.next()
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}
			return err
		}
		switch r.Event {
		case "error":
			fmt.Fprintf(cmd.ErrOrStderr(), "error: %s\n", r.Err)
		case "status":
			if !first || r.Status != nil {
				if err := show(cmd, r.Status); err != nil {
					return err
				}
			}
		case "message":
			fmt.Fprintln(cmd.OutOrStdout(), string(r.Msg))
		}
		first = false

		if ctx.Err() != nil {
			return nil
		}
	}
}

// show prints a status, with an activation code the way somebody standing at the device needs it.
func show(cmd *cobra.Command, s *status) error {
	if s == nil {
		return nil
	}
	out := cmd.OutOrStdout()

	state := s.State
	if s.Detail != "" {
		state += ": " + s.Detail
	}
	if s.Code != "" {
		fmt.Fprintf(out, "%s\n    type %s at xiaozhi.me\n", state, s.Code)
	} else {
		fmt.Fprintln(out, state)
	}
	if s.Session == "" {
		return nil
	}

	up := ""
	if since, err := time.Parse(time.RFC3339, s.Since); err == nil {
		up = fmt.Sprintf(", up %s", time.Since(since).Round(time.Second))
	}
	fmt.Fprintf(out, "    session %s on %s\n    %d Hz up, %d Hz down%s\n",
		s.Session, s.URL, s.Uplink, s.Downlink, up)
	fmt.Fprintf(out, "    %d messages (%d text, %d audio, %d bytes), %d pings\n",
		s.Messages, s.Text, s.Audio, s.Bytes, s.Pings)
	return nil
}
