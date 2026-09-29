package xiaozhi

// One turn: the microphone open, Opus going up, and the listen that closes it.
//
// A turn is the unit the protocol is built in — a listen start, some audio, a listen stop — and it
// is also the unit this device's microphone is shared on. It is a type rather than a flag on the
// feature because there is exactly one at a time and every way of ending one has to reach the same
// place: a terminal's stop, an abort, a dropped session, and the turn deciding for itself that
// the speaker has finished all end the same two calls.
//
// What opens a turn is M5's decision. Before it, a turn is opened by hand over the control socket,
// which is why the shape here is the one a wake word will call rather than one a terminal needed:
// the whole of feature/voice's endpointing and pre-roll arrives with that wiring, and none of it
// belongs in a turn nobody can start by saying a word.

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"time"

	"github.com/HuskerMinion/techo5/echod/internal/config"
	"github.com/HuskerMinion/techo5/echod/internal/hardware/mic"
	"github.com/HuskerMinion/techo5/echod/internal/lib/endpoint"
	"github.com/HuskerMinion/techo5/echod/internal/lib/safe"
)

// turn is an open listen: the microphone, the encoder, and how it is ended.
type turn struct {
	sess *Session
	up   *uplink

	// id numbers it, mode is who ends the utterance, and manual is the same answer kept as a
	// boolean because it is asked on every frame.
	id     int
	mode   string
	manual bool

	// sent and bytes are the session's own counters as they stood when this turn opened, so what
	// went up in this turn can be told from what went up in the last one on the same session.
	sent  int
	bytes int

	// at is when it opened. end is how far into the turn the speaker finished, and is only known
	// when this device was the one measuring it.
	at  time.Time
	end time.Duration

	cancel context.CancelFunc
	ended  chan struct{}
}

// Turn is one listen: what it sent and how it went. It is what the log line says in full, and
// what a terminal watching the client is sent when the turn ends.
type Turn struct {
	// ID numbers the turns, so a terminal that asked for one can tell that one ending from
	// another one starting. Two terminals driving the same client is not unusual, and both
	// watching both turns is the honest reading of what they can see.
	ID int `json:"id"`

	// State is why the turn ended. Empty on the way out, where a turn has only just been opened.
	State string `json:"state,omitempty"`

	// Mode is who ended the utterance: the server's own detection, or this device's endpoint.
	Mode string `json:"mode,omitempty"`

	// Frames and Bytes are the packets this turn pushed, and Kbps is what that came to — the
	// "actual" column of the plan's table, measured on the device rather than assumed.
	Frames  int     `json:"frames"`
	Bytes   int     `json:"bytes"`
	Kbps    float64 `json:"kbps,omitempty"`
	Seconds float64 `json:"seconds,omitempty"`

	// EncodePct is the encoder's share of one core over the turn, which is the unit the plan
	// measured it in. The turn's own length is the denominator rather than the process's, so this
	// is the same number a person with top would have read off the device.
	EncodePct float64 `json:"encode_cpu_pct,omitempty"`

	// EndSpeech is how far into the turn the speaker finished, for a turn this device ended.
	// Zero when the server decided, or when nothing was said at all.
	EndSpeech float64 `json:"end_speech_s,omitempty"`

	// Pending is the audio left in an unfinished packet, which the turn dropped rather than
	// sending as a short one. It is worth having: a turn that ends on an odd frame is a turn that
	// loses up to one frame's worth of tail, and this is how much.
	Pending int `json:"pending_samples,omitempty"`
}

// Listen opens the microphone and starts sending.
//
// mode is the protocol's own word for who ends an utterance: ListenAuto leaves it to the server's
// voice activity detection, and anything else ends the turn here, on the device's own endpoint,
// followed by an explicit stop. The two are not interchangeable — with the server deciding, an
// endpoint of our own would end turns it is still listening for, and the two would argue about
// where the sentence was.
func (f *Feature) Listen(mode string) error {
	f.mu.Lock()
	sess, live := f.sess, f.turn
	f.mu.Unlock()

	if sess == nil {
		return fmt.Errorf("no xiaozhi session: it is %s", f.Status().State)
	}
	if live != nil {
		return errors.New("a turn is already open on this session; stop it before starting another")
	}

	// The other backend is asked to let go before the microphone is opened rather than after, so
	// there is no window in which two of them are listening to the same room. A terminal's
	// `xiaozhi listen` goes through here too, which is the point: a device with two assistants
	// arbitrates on every path that opens a microphone, not only on the one a wake word uses.
	yieldMicrophone()

	up, err := newUplink(sess)
	if err != nil {
		return err
	}
	// Sent before the first packet rather than with it, so a terminal that hears the turn open and
	// the turn end gets the same count the log will say.
	if err := sess.Listen(ListenStart, mode); err != nil {
		return err
	}

	st := sess.Stats()
	ctx, cancel := context.WithCancel(context.Background())
	t := &turn{
		sess: sess, up: up, id: int(f.turns.Add(1)),
		mode: mode, manual: mode != ListenAuto,
		sent: st.Sent, bytes: st.SentBytes,
		at: time.Now(), cancel: cancel, ended: make(chan struct{}),
	}

	f.mu.Lock()
	// The session can go between the check above and here — it is closed from another goroutine,
	// and a turn attached to a dead socket is one that nothing will ever end.
	if f.sess != sess {
		f.mu.Unlock()
		cancel()
		return errors.New("the session ended as the turn was opening")
	}
	f.turn = t
	f.mu.Unlock()

	c := config.Get().Xiaozhi
	slog.Info("xiaozhi: listening",
		"turn", t.id, "mode", mode, "frame_ms", c.Frame(),
		"kbps", c.Bitrate/1000, "complexity", complexity(c.Complexity))
	// The acknowledgement first and the status after it, because a terminal waiting for one reads
	// the first line either of them produces. Answering the request is the reply; the sensor
	// catching up is a consequence of it.
	f.control.broadcast(reply{Event: eventGoodbye, Turn: &Turn{ID: t.id, Mode: mode}})
	f.touch()

	safe.Go("xiaozhi uplink", func() { f.stream(ctx, t) })
	return nil
}

// Stop ends the turn that is open and silences whatever the device is saying, and returns once the
// turn is over.
//
// It is a no-op when there is neither, because stop has to work when there is nothing to stop: a
// button is pressed without the device being asked what it is doing, and an error there would be
// the first thing anybody ever saw of this backend.
//
// The silence is not conditional on the turn, and that is the whole reason this is one function
// and not two. The longest answers are the ones with no turn behind them — the microphone finished
// sending seconds ago and the cloud is still talking — so a stop that only cancelled a turn would
// be silent exactly when it is most needed: on a barge-in, which is the one moment somebody is
// trying to talk over the device. It is asked of the downlink whether or not a turn exists.
//
// Why it was asked for is not recorded here. The caller knows — a terminal, an abort, a session
// going away — and the log line the caller writes is where that belongs.
func (f *Feature) Stop() {
	f.mu.Lock()
	t, down := f.turn, f.down
	f.mu.Unlock()

	// Before the turn, and not after it. Cancelling first means the uplink goroutine stops asking
	// for packets while this waits, so the two are not fighting over the same silence: the downlink
	// is hushed first and every packet the turn sends after that is dropped, which is the same
	// outcome a barge-in wants and reached in one order rather than two.
	if down != nil {
		down.stop("it was interrupted")
	}
	if t == nil {
		return
	}

	t.cancel()
	<-t.ended
}

// Barge is somebody reaching for a button to make the device stop answering, and it reports whether
// it did.
//
// The report is the point. A press of the action button means "make it stop" while something is
// being said and "ask me something" while nothing is, and the feature that owns the button cannot
// tell those apart on its own: the answer is attached to the speaker, not to the pipeline that
// press otherwise acts on, so a caller that went on to open a turn would have stopped the answer and
// started listening in the same press. Returning false for "there was nothing of mine to stop" is
// what lets the press fall through to the other thing it can mean.
//
// Both states count, and the open turn is the one that is easy to leave out. An answer being said is
// the visible half; a turn with the microphone open is the half that leaves the device deaf if it is
// missed, and it is silent by definition — the room is waiting for the device to notice the press.
// A turn also outlives the answer it asked for, so there is a stretch of a turn where the device is
// neither listening nor speaking and only the turn itself says the microphone is open.
//
// Both halves happen, and neither is enough on its own. Stop is what the room hears, and it works
// with no turn behind it — the usual case here, because the longest answers have none, the
// microphone finished sending seconds ago. The message is what the server needs, so it stops
// synthesising an answer nobody is going to hear, and it is best-effort because a button press is
// not a moment to report a network failure over: the silence has already happened either way.
func (f *Feature) Barge() bool {
	down := f.downlink()
	speaking := down != nil && down.isSpeaking()
	listening := f.Listening()
	if !speaking && !listening {
		return false
	}
	f.Stop()

	slog.Info("xiaozhi: the action button stopped an answer", "listening", listening)
	if sess, err := f.session(); err == nil {
		_ = sess.Abort(AbortButton)
	}
	return true
}

// stream is the turn: frames from the microphone, packets to the server, until something ends it.
//
// It owns the turn from here to close(t.ended), which is what makes every other way of ending one
// the same two calls, and it is the only writer of t.up.
func (f *Feature) stream(ctx context.Context, t *turn) {
	defer close(t.ended)

	frames, unlisten := micListen("xiaozhi")
	defer unlisten()

	// The end of the utterance, for a turn this device ends itself. Left nil otherwise, so a
	// server doing its own detection is not argued with.
	var ep *endpoint.Detector
	if t.manual {
		ep = endpoint.New(endpoint.Default)
	}

	why := "canceled"
	defer func() { f.closeTurn(t, why) }()

	for {
		select {
		case <-ctx.Done():
			return

		case frame, ok := <-frames:
			if !ok {
				// The array was let go under this: the daemon is shutting down, or the mute came
				// off and the capture device was handed back. Nothing is going up and nothing will.
				why = "the microphone went away"
				return
			}

			if err := t.up.feed(frame); err != nil {
				why = err.Error()
				return
			}

			if ep != nil && ep.Feed(frame) {
				why = "the speaker finished"
				t.end = time.Duration(ep.EndedAt()) * time.Second / mic.Rate
				return
			}
		}
	}
}

// closeTurn says how the turn went, and lets the next one start.
//
// The turn is cleared before anything is said about it, so a terminal that starts another one off
// the event is not racing the bookkeeping for this one.
func (f *Feature) closeTurn(t *turn, why string) {
	f.mu.Lock()
	if f.turn == t {
		f.turn = nil
	}
	down := f.down
	f.mu.Unlock()

	// Where the answer starts from. The cloud's latency is measured from the end of what was sent
	// to the first packet of the reply, and this is the device's half of that instant — the same
	// one M0 measured the 1.758 s to 2.197 s against. It is stamped after the turn is cleared so a
	// terminal that starts another one off the event is not racing the bookkeeping for this one.
	if down != nil {
		down.heard(time.Now())
	}

	// A turn that was sending has to say so, whatever ended it. In the automatic mode the
	// server's own detection has usually closed the listen already, and an explicit stop after
	// that is a no-op rather than an error — but leaving the listen open is not, so it goes out
	// whenever there is still a socket to send it on. A session that has gone fails here, and is
	// not worth a warning: it has already said so.
	if err := t.sess.Listen(ListenStop, t.mode); err != nil {
		slog.Debug("xiaozhi: the listen could not be closed", "turn", t.id, "err", err)
	}

	took := time.Since(t.at)
	st := t.sess.Stats()
	out := &Turn{
		ID: t.id, State: why, Mode: t.mode,
		Frames:  st.Sent - t.sent,
		Bytes:   st.SentBytes - t.bytes,
		Seconds: round(took.Seconds()),
		Pending: t.up.pending(),
	}
	if out.Seconds > 0 {
		out.Kbps = round(float64(out.Bytes) * 8 / out.Seconds / 1000)
		out.EncodePct = round(t.up.encode.Seconds() / out.Seconds * 100)
	}
	if t.end > 0 {
		out.EndSpeech = round(t.end.Seconds())
	}

	slog.Info("xiaozhi: turn ended",
		"turn", out.ID, "why", why, "mode", t.mode,
		"frames", out.Frames, "bytes", out.Bytes, "kbps", out.Kbps,
		"seconds", out.Seconds, "encode_cpu_pct", out.EncodePct,
		"end_speech_s", out.EndSpeech, "pending_samples", out.Pending)
	f.touch()
	f.control.broadcast(reply{Event: eventTurn, Turn: out})
}

// touch republishes the status, for a turn opening or closing.
//
// The state both a person and a terminal read has to show a turn that is running, and this is the
// only status there is — so a change to it republishes it whole rather than keeping a second copy
// that could disagree. Read back through Status rather than off the field, so the republish
// carries the counters as they are now and not as they were when the session opened.
func (f *Feature) touch() { f.set(f.Status()) }

// round is to hundredths, which is finer than any of these numbers mean and coarser than a float
// has room to be interesting in a log.
func round(v float64) float64 { return math.Round(v*100) / 100 }
