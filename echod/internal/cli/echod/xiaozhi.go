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
			"server says — transcripts, replies, speech states, activations — and how each\n" +
			"turn went, until interrupted, which is how a ten-minute hold is checked without a\n" +
			"screen.\n\n" +
			"`listen --once` is a whole turn: it opens the microphone, streams what it hears\n" +
			"up as Opus, prints the transcript the service heard, and ends itself when you stop\n" +
			"talking. It reports the packets sent, the bitrate that came to, and the encoder's\n" +
			"share of a core — the three numbers the codec was chosen on.\n\n" +
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
					// than a state is what a person means by it, and a turn the device ends itself is
					// the only one that can be waited for.
					return ctl.turn(cmd, request{Cmd: "listen", State: "start", Mode: "manual"})
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
	c.Flags().BoolVar(&once, "once", false, "for listen: one turn ended by the device, rather than holding the microphone open")
	c.Flags().StringVar(&mode, "mode", "auto", "for listen: auto leaves the end of an utterance to the server; --once overrides it")
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
	State     string `json:"state"`
	Detail    string `json:"detail,omitempty"`
	Host      string `json:"host,omitempty"`
	URL       string `json:"url,omitempty"`
	Code      string `json:"activation_code,omitempty"`
	Session   string `json:"session,omitempty"`
	Uplink    int    `json:"uplink_hz"`
	Downlink  int    `json:"downlink_hz"`
	Since     string `json:"since"`
	Listening bool   `json:"listening"`
	Speaking  bool   `json:"speaking"`
	Stats     stats  `json:"stats"`
}

// stats is the session's counters, nested because the daemon nests them.
//
// The dotted keys this used to carry ("stats.sent") are viper's, not encoding/json's: json looks
// for a field literally named "stats.sent" and the daemon sends a nested object, so every counter
// silently stayed zero while the turn metrics beside it, which are nested properly, were correct.
type stats struct {
	Messages  int `json:"messages"`
	Text      int `json:"text"`
	Audio     int `json:"audio"`
	Bytes     int `json:"bytes"`
	Sent      int `json:"sent"`
	SentBytes int `json:"sent_bytes"`
	Pings     int `json:"pings"`
}

// turn is one listen, as the daemon reports it. Mirrored rather than imported for the reason the
// other wire types here are: this binary has to be able to talk to a daemon that is older than it.
type turn struct {
	ID        int     `json:"id"`
	State     string  `json:"state,omitempty"`
	Mode      string  `json:"mode,omitempty"`
	Frames    int     `json:"frames"`
	Bytes     int     `json:"bytes"`
	Kbps      float64 `json:"kbps,omitempty"`
	Seconds   float64 `json:"seconds,omitempty"`
	EncodePct float64 `json:"encode_cpu_pct,omitempty"`
	EndSpeech float64 `json:"end_speech_s,omitempty"`
	Pending   int     `json:"pending_samples,omitempty"`
}

// speech is one answer, as the daemon reports it: the other half of a turn, and the numbers the
// other half of the plan's table was measured in.
//
// It is a separate event and not a field of the turn because the two end at different moments. The
// turn ends when the microphone's last packet has gone out, and the answer to it arrives after
// that and takes as long as the cloud takes, so a turn event carrying a speech would be a turn that
// cannot be reported until the device has finished speaking.
type speech struct {
	ID        int     `json:"id"`
	State     string  `json:"state,omitempty"`
	Packets   int     `json:"packets"`
	Bytes     int     `json:"bytes"`
	Kbps      float64 `json:"kbps,omitempty"`
	Seconds   float64 `json:"seconds,omitempty"`
	DecodePct float64 `json:"decode_cpu_pct,omitempty"`
	Latency   float64 `json:"latency_s,omitempty"`
	Peak      float64 `json:"peak,omitempty"`
	Failed    int     `json:"failed,omitempty"`
	Late      int     `json:"late,omitempty"`
	Dropped   int     `json:"dropped,omitempty"`
}

type reply struct {
	Event  string          `json:"event"`
	Status *status         `json:"status"`
	Msg    json.RawMessage `json:"msg"`
	Turn   *turn           `json:"turn"`
	Speech *speech         `json:"speech"`
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
		case "turn":
			// A listen that was opened rather than a status asked for. The turn is running; this
			// terminal is not going to follow it, so it is said and left.
			return showTurn(cmd, r.Turn)
		case "speech":
			// An answer that ended before the status did. A daemon that holds a session open has
			// nothing else to say, so this is the last line it will send until somebody speaks.
			return showSpeech(cmd, r.Speech)
		case "goodbye":
			fmt.Fprintf(cmd.OutOrStdout(), "%s: ok\n", req.Cmd)
			return nil
		}
	}
}

// turn opens one listen and follows it to its end.
//
// ask is the wrong shape for this. The answer to a listen start is immediate, and everything worth
// reading arrives after it — the transcript, the reply, the packets the device says it sent — so a
// command that returned on the acknowledgement would report nothing about the turn it caused.
//
// It ends on the daemon's report of *this* turn rather than on the first one to end, because a
// second terminal driving the same client opens turns this one is told about too. The id in the
// acknowledgement is what tells them apart.
func (c *control) turn(cmd *cobra.Command, req request) error {
	out := cmd.OutOrStdout()
	if err := c.send(req); err != nil {
		return err
	}

	var id int
	for {
		r, err := c.next()
		if err != nil {
			return err
		}
		switch r.Event {
		case "error":
			return errors.New(r.Err)
		case "goodbye":
			if r.Turn == nil {
				// A daemon that knows nothing about turns has taken the request and will not say
				// when it is over. Say so rather than waiting for an event that cannot come.
				return errors.New("the daemon did not report a turn, so there is nothing to wait for")
			}
			id = r.Turn.ID
			fmt.Fprintf(out, "listening (turn %d, ended by the device): speak now\n", id)
		case "message":
			fmt.Fprintln(out, string(r.Msg))
		case "turn":
			if r.Turn == nil || r.Turn.ID != id {
				// Somebody else's turn, or one that ended while this was being asked for.
				continue
			}
			return showTurn(cmd, r.Turn)
		case "speech":
			// The answer to a sentence the server sent while the microphone was still open. It
			// is not this turn's to report — the turn has not ended yet — but it happened, and
			// dropping it would leave the one line that says the device spoke.
			showSpeech(cmd, r.Speech)
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
		case "turn":
			showTurn(cmd, r.Turn)
		case "speech":
			showSpeech(cmd, r.Speech)
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
	if s.Listening {
		up += ", listening"
	}
	if s.Speaking {
		up += ", speaking"
	}
	fmt.Fprintf(out, "    session %s on %s\n    %d Hz up, %d Hz down%s\n",
		s.Session, s.URL, s.Uplink, s.Downlink, up)
	fmt.Fprintf(out, "    %d messages (%d text, %d audio, %d bytes), %d pings\n",
		s.Stats.Messages, s.Stats.Text, s.Stats.Audio, s.Stats.Bytes, s.Stats.Pings)
	fmt.Fprintf(out, "    sent %d packets, %d bytes\n", s.Stats.Sent, s.Stats.SentBytes)
	return nil
}

// showTurn prints how one listen went. The numbers are the ones the plan's table was measured in:
// the packets the device pushed, the rate that came to, and the encoder's share of a core.
//
// A turn with nothing in it is a turn where the microphone heard nothing, or where every frame
// failed to go out, and the two are told apart by the device's own log rather than from here.
func showTurn(cmd *cobra.Command, t *turn) error {
	if t == nil {
		return nil
	}
	out := cmd.OutOrStdout()

	if t.State == "" {
		return nil
	}
	fmt.Fprintf(out, "turn %d ended: %s\n", t.ID, t.State)
	if t.Frames == 0 {
		fmt.Fprintln(out, "    no audio was sent")
		return nil
	}
	fmt.Fprintf(out, "    %d packets, %d bytes", t.Frames, t.Bytes)
	if t.Kbps > 0 {
		fmt.Fprintf(out, " at %.1f kbps", t.Kbps)
	}
	fmt.Fprintf(out, " over %.1fs", t.Seconds)
	if t.EncodePct > 0 {
		fmt.Fprintf(out, ", encoder %.1f%% of a core", t.EncodePct)
	}
	if t.EndSpeech > 0 {
		fmt.Fprintf(out, "\n    speech finished at %.1fs", t.EndSpeech)
	}
	if t.Pending > 0 {
		fmt.Fprintf(out, ", %d samples left unsent", t.Pending)
	}
	fmt.Fprintln(out)
	return nil
}

// showSpeech prints how one answer went. It is the downlink's half of the same table showTurn
// prints the uplink's: what the device received, what rate that came to, what the decoder cost, and
// how long the cloud took to start.
//
// The latency is the cloud's own, measured from the end of the utterance to the first packet of the
// answer, and it is the figure M0 measured against. It is not the delay in the room: that one also
// carries this device's cushion and the time the speaker hardware keeps sounding, and the two are
// the difference between a number in a plan and what a person hears.
func showSpeech(cmd *cobra.Command, s *speech) error {
	if s == nil || s.State == "" {
		return nil
	}
	out := cmd.OutOrStdout()

	fmt.Fprintf(out, "speech %d ended: %s\n", s.ID, s.State)
	if s.Packets == 0 {
		fmt.Fprintln(out, "    no audio was received")
		return nil
	}
	fmt.Fprintf(out, "    %d packets, %d bytes", s.Packets, s.Bytes)
	if s.Kbps > 0 {
		fmt.Fprintf(out, " at %.1f kbps", s.Kbps)
	}
	fmt.Fprintf(out, " over %.1fs", s.Seconds)
	if s.DecodePct > 0 {
		fmt.Fprintf(out, ", decoder %.1f%% of a core", s.DecodePct)
	}
	fmt.Fprintln(out)
	if s.Latency > 0 {
		fmt.Fprintf(out, "    cloud took %.2fs to start answering\n", s.Latency)
	}
	if s.Peak > 0 {
		fmt.Fprintf(out, "    peak %.0f%% of full scale\n", s.Peak*100)
	}
	if s.Late > 0 || s.Dropped > 0 || s.Failed > 0 {
		fmt.Fprintf(out, "    %d late, %d dropped, %d failed\n", s.Late, s.Dropped, s.Failed)
	}
	return nil
}
