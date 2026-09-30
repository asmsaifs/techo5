package xiaozhi

// What opens a turn, now that a wake word can. The decision of which backend is live belongs to
// feature/voice, which owns the wake word and the action button and can see the other backend's
// turn; this file is the side that answers "ask me" and "stand down" without knowing what asked.
//
// The two directions are here rather than split across packages because they are the same invariant
// seen from two ends: at most one backend holds the microphone, and whoever is holding it is the one
// the last switch said. A turn cannot check the other backend's turn itself — the other backend is
// the thing that would have to ask.

import (
	"errors"
	"log/slog"
	"sync"
	"time"

	"github.com/HuskerMinion/techo5/echod/internal/config"
	"github.com/HuskerMinion/techo5/echod/internal/hardware/mic"
)

// wakeTailSkip is the least a wake-triggered turn throws away before it sends anything; see
// wakeGate for the rest. A var so wake_test.go can turn the whole thing off: the arbitration tests
// feed a handful of frames to count packets, not to be discarded.
var wakeTailSkip = 200 * time.Millisecond

const (
	// wakeWordLevel is how loud a frame has to be (rms) to be the word rather than the room. Read
	// off the Show: the room sat near 700 and the wake word peaked near 4000.
	wakeWordLevel = 1500

	// wakeLookFrames is how long the gate waits for the word to start, and wakeWordFrames the
	// longest it treats one as lasting; both in 20 ms frames. A word that never comes, or one that
	// runs on into a request, must not eat the request.
	wakeLookFrames = 75 // 1.5 s
	wakeWordFrames = 45 // 0.9 s

	// wakeQuietFrames is the gap that ends the word: 200 ms (a dip inside "Alexa" is shorter) below a third of its peak.
	wakeQuietFrames = 10
)

// wakeGate decides which of a wake turn's first frames are still the wake word.
//
// Measured on the Show, a trigger can land before the word is over in the audio a turn hears:
// the first half second was the room, and the word came after it. Any fixed skip either clears
// the word too little or eats the request, so the gate follows the word instead: it discards at
// least min frames, then, if a burst of speech begins within wakeLookFrames, everything through the
// gap that ends it. The cloud transcribes a word it is sent, and "Alexa" comes back as the first
// word of the request.
type wakeGate struct {
	min    int
	n      int
	inWord bool
	open   bool
	peak   float64
	word   int
	quiet  int

	// floor is the quietest 100 ms seen so far, which is the room: music or a fan lifts it, and
	// the word has to stand out of that and not out of silence.
	floor float64
	acc   float64
	accN  int
}

func newWakeGate(skip time.Duration) *wakeGate {
	return &wakeGate{min: int(skip / (time.Second * mic.FrameSamples / mic.Rate))}
}

// discard reports whether this frame is the wake word's and should not be sent.
func (g *wakeGate) discard(frame []int16) bool {
	if g.open {
		return false
	}
	g.n++
	r := rms(frame)
	if !g.inWord {
		g.acc += r
		if g.accN++; g.accN == 5 {
			if l := g.acc / 5; g.floor == 0 || l < g.floor {
				g.floor = l
			}
			g.acc, g.accN = 0, 0
		}
		level := float64(wakeWordLevel)
		if g.floor > 0 {
			level = max(level, 2.5*g.floor)
		} else if g.n <= 5 {
			// Nothing to compare against yet: the first 100 ms is taken as the room.
			return true
		}
		if r > level {
			g.inWord, g.peak, g.word = true, r, 1
			return true
		}
		if g.n > g.min && g.n > wakeLookFrames {
			g.open = true
			return false
		}
		return true
	}
	g.word++
	g.peak = max(g.peak, r)
	if r < 0.35*g.peak {
		g.quiet++
	} else {
		g.quiet = 0
	}
	if g.quiet >= wakeQuietFrames || g.word >= wakeWordFrames {
		g.open = true
		slog.Info("xiaozhi: the wake word ended", "after_ms", g.n*20, "word_ms", g.word*20)
	}
	return true
}

// yielder is the other backend, asked rather than called.
//
// A hook rather than an import, because feature/voice already imports this package to reach the
// speaker and the switch: importing it the other way would be a cycle, and a cycle between two
// features that are each other's peer is the one thing that would make them impossible to move
// apart later. The registration happens once, in voice's build.
var yielder struct {
	mu sync.Mutex
	fn func()
}

// YieldMic registers what has to be done before this backend takes the microphone.
//
// A turn asks for it on every path that opens one, including a terminal's `xiaozhi listen`: a
// device with two assistants and one microphone cannot arbitrate on the paths that were wired up
// first.
//
// One direction only. The microphone is given back by the turn that took it, the same way every
// other listener in this device lets it go — by unlistening — and there is nothing for the other
// backend to be told on the way out, because it asked nothing and is owed nothing. A handback
// callback here would be a second way to do a thing that already has one, and the two would drift.
func YieldMic(take func()) {
	yielder.mu.Lock()
	defer yielder.mu.Unlock()
	yielder.fn = take
}

// yieldMicrophone is what a turn calls before it opens one.
func yieldMicrophone() {
	yielder.mu.Lock()
	fn := yielder.fn
	yielder.mu.Unlock()
	if fn != nil {
		fn()
	}
}

// standDown ends this backend's turn and silences it, for a switch that has moved elsewhere.
//
// It is the same two calls Stop makes and returns the same way, and it is a no-op when there is
// nothing running: a switch is a thing a person moves on a device that is usually idle, and a
// switch moved while nothing is happening must not produce an error or a line in the log saying
// something was cut short.
//
// The client is left running. It holds a socket and nothing else, and the next switch back should
// not have to wait out a reconnect to find out whether the cloud is there.
func (f *Feature) StandDown() bool {
	f.mu.Lock()
	live := f.turn != nil
	down := f.down
	f.mu.Unlock()

	if !live && (down == nil || !down.isSpeaking()) {
		return false
	}
	f.Stop()
	slog.Info("xiaozhi: the voice backend moved, ending the turn")
	return true
}

// Listening reports whether a turn is open, which is the difference between a turn running and an
// answer being said: the first holds the microphone, the second only the speaker.
func (f *Feature) Listening() bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.turn != nil
}

// Cancel ends a turn and leaves the speaker alone, for a gesture that turned out to mean something
// else rather than for a press that meant "stop".
//
// It is Stop without the downlink. A canceled turn is one that never produced an answer, so there is
// nothing on the speaker to silence and reaching for it would be a claim taken and released for no
// reason. The listen is still closed properly: that is closeTurn's, and it is the half that tells
// the server the utterance is over.
func (f *Feature) Cancel() {
	f.mu.Lock()
	t := f.turn
	f.mu.Unlock()
	if t == nil {
		return
	}
	t.cancel()
	<-t.ended
}

// BackendReady reports whether a turn could be opened at all, and says why not when it could not.
//
// The reason is in the returned string rather than only in a log because the caller is deciding
// whether to tell the room: a wake word that reaches a device with no session should say so rather
// than look like it was not heard, and the sentence that says it is different for "not connected"
// and "not switched on".
func (f *Feature) BackendReady() (bool, string) {
	switch st := f.Status(); st.State {
	case StateConnected:
		return true, ""
	case StateOff:
		return false, "the xiaozhi client is off"
	case StateActivating:
		return false, "it is waiting for the activation code"
	case StateConnecting:
		return false, "it is connecting"
	default:
		return false, "it is " + st.State
	}
}

// wakeMode is who ends the utterance of a turn opened by a wake word.
//
// The same setting decides it for the Home Assistant pipeline, and it is read here rather than
// taken as an argument because a turn from a wake word and a turn from a terminal are the same turn:
// the setting is about how this device ends a sentence, not about who asked for one. With the server
// deciding, this device sends audio until the server stops asking; with the device deciding, its own
// endpoint closes the listen and follows it with an explicit stop.
func wakeMode() string {
	if config.Get().Microphone.PipelineEnds {
		return ListenAuto
	}
	return ListenManual
}

// Wake opens a turn as a wake word would, and reports whether it did.
//
// It is a separate entry point from Listen rather than a call to it because a wake word means
// something a terminal's request does not: the room is talking, and the device is interrupting
// whatever it was doing to listen. So an answer still being said is stopped first — a microphone
// opening under a speaker that has not been silenced is the device talking over itself, and the
// canceller cannot undo that — and a turn already open is left alone, because a wake word in the
// middle of a sentence somebody is still saying is part of that sentence rather than a new request.
//
// Neither of those is enforced here beyond the ordering. The microphone is a shared resource with
// another backend, and taking it is a conversation with that backend, which is what yieldMicrophone
// is.
func (f *Feature) Wake() error {
	if ready, why := f.BackendReady(); !ready {
		return errors.New("xiaozhi cannot take a turn: " + why)
	}

	// An answer first. It is the speaker that has to be free, and it can be freed without touching
	// the turn the turn itself has already closed.
	if down := f.downlink(); down != nil && down.isSpeaking() {
		slog.Info("xiaozhi: a wake word interrupted an answer")
		f.Stop()
	}

	// A turn already open means somebody is still talking. Saying the wake word now is part of
	// their sentence, and opening a second listen would be the recognizer's problem as well as the
	// microphone's.
	f.mu.Lock()
	live := f.turn
	f.mu.Unlock()
	if live != nil && live.followUp {
		// Nobody asked for a follow-up, so the wake word is the request and not a part of one.
		slog.Info("xiaozhi: a wake word replaced a follow-up")
		f.Cancel()
	} else if live != nil {
		return errors.New("a turn is already open; the wake word is part of the sentence")
	}
	return f.listen(wakeMode(), wakeTailSkip, 0)
}

// answered is what feature/voice registers to hear that an answer has finished playing, so it can
// decide whether the room gets a follow-up. A hook rather than an import for the same reason as
// YieldMic.
var answered struct {
	mu sync.Mutex
	fn func()
}

// OnAnswered registers what is called after an answer has been heard to the end.
func OnAnswered(fn func()) {
	answered.mu.Lock()
	defer answered.mu.Unlock()
	answered.fn = fn
}

// FollowUp opens a turn for d with no wake word behind it, and reports whether it did. The server
// ends the utterance, and nothing said within d ends the turn: hearing nothing is a normal ending.
func (f *Feature) FollowUp(d time.Duration) error {
	if ready, why := f.BackendReady(); !ready {
		return errors.New("xiaozhi cannot take a turn: " + why)
	}
	if f.Listening() {
		return errors.New("a turn is already open")
	}
	slog.Info("xiaozhi: listening again after the answer", "for", d)
	return f.open(ListenAuto, 0, d, true)
}
