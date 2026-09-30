package xiaozhi

// The other backend, answered.
//
// These are the calls feature/voice makes on this side, and they are the ones that decide whether a
// switch can be moved mid-turn. What each has to do is small to say and easy to get wrong: yield the
// microphone before a turn, stand down when the switch moves, open a turn for a wake word, and say
// so when it cannot.
//
// What they are not is a test of the switch itself. The switch is in feature/voice, which cannot be
// built on this host and is exercised on the device. What is testable here is the contract those
// calls are held to, and the contract is where the wedging would live: a backend that did not let go
// of a microphone, or a turn that outlived the button that stopped it.

import (
	"strings"
	"testing"
	"time"

	"github.com/HuskerMinion/techo5/echod/internal/config"
)

// yielded is a stand-in for feature/voice's side of the handover.
//
// A closure rather than the real one because this cannot import feature/voice — that import is the
// cycle YieldMic exists to avoid — and because what is being checked is that it was called before
// the array was taken, which is the part the other side is responsible for. Whether it ended a Home
// Assistant turn is that package's own business, tested there.
func yielded(t *testing.T) *int {
	t.Helper()
	calls := new(int)
	old := yielder.fn
	YieldMic(func() { *calls++ })
	t.Cleanup(func() {
		YieldMic(old)
	})
	return calls
}

// A wake word is a turn, opened the same way any turn is.
func TestWakeOpensATurn(t *testing.T) {
	cl := connect(t)

	if err := cl.f.Wake(); err != nil {
		t.Fatalf("a wake word on a connected client: %v", err)
	}
	cl.waitHeld(t, true, "a turn opened by a wake word is listening")
	if !cl.f.Listening() {
		t.Error("Listening is false with a turn open")
	}

	// The mode is this device's setting, not a constant, and the setting is the same one the Home
	// Assistant pipeline ends on. So a turn opened by a wake word has to agree with the terminal's
	// view of the mode, which is the only place the value is visible from outside.
	_, turn := cl.ask(t, `{"cmd":"listen","state":"stop"}`)
	if turn == nil {
		t.Fatal("stopping the turn did not report the turn it stopped")
	}
	if want := wakeMode(); turn.Mode != want {
		t.Errorf("the turn is in %q mode, want %q from PipelineEnds=%v",
			turn.Mode, want, config.Get().Microphone.PipelineEnds)
	}
}

// The mode follows the setting, in both directions, because it is read rather than hardcoded and a
// hardcoded one is exactly the bug this catches.
func TestTheModeFollowsTheSettingThatEndsASentence(t *testing.T) {
	for name, tc := range map[string]struct {
		serverEnds bool
		want       string
	}{
		"the server ends it":  {true, ListenAuto},
		"this device ends it": {false, manualMode},
	} {
		t.Run(name, func(t *testing.T) {
			// DeviceEnds rather than PipelineEnds, because the writer inverts it — the two names
			// answer opposite questions about the same field, and the one the writer is called is the
			// one a person reads.
			if err := config.Set().Microphone().DeviceEnds(!tc.serverEnds); err != nil {
				t.Fatalf("setting who ends the utterance: %v", err)
			}
			if got := wakeMode(); got != tc.want {
				t.Errorf("a wake word turn is in %q mode, want %q", got, tc.want)
			}
		})
	}
}

// The handover is asked for before the array is taken, on every path that opens a turn. A path that
// skipped it is a path where two backends can hold one microphone, and it would be the terminal's
// path, which is the one nobody thinks about because nobody is holding a button when they use it.
func TestEveryPathToATurnAsksForTheMicrophoneFirst(t *testing.T) {
	for name, open := range map[string]func(*client) error{
		"a wake word": func(cl *client) error { return cl.f.Wake() },
		"a terminal": func(cl *client) error {
			_, err := cl.term.ask(`{"cmd":"listen","state":"start"}`)
			return err
		},
	} {
		t.Run(name, func(t *testing.T) {
			cl := connect(t)
			calls := yielded(t)

			if err := open(cl); err != nil {
				t.Fatalf("opening a turn: %v", err)
			}
			cl.waitHeld(t, true, "the turn took the microphone")

			if *calls != 1 {
				t.Errorf("the other backend was asked for the microphone %d times, want once", *calls)
			}
		})
	}
}

// The switch moving mid-turn. This is the case the whole arrangement exists for, and the one a
// person at a settings sheet or Home Assistant will do without thinking: move the switch while the
// other backend is answering.
//
// What has to be true afterwards is that the microphone is free, because the backend that was
// switched to is about to claim it. Not that the turn is tidy, and not that the server agrees — the
// utterance is abandoned mid-word and the server is told it was.
func TestStandDownEndsATurnAndLetsTheMicrophoneGo(t *testing.T) {
	cl := connect(t)
	calls := yielded(t)

	if err := cl.f.Wake(); err != nil {
		t.Fatalf("opening a turn to interrupt: %v", err)
	}
	cl.waitHeld(t, true, "the turn is running")
	cl.speech(t, 20)
	cl.cloud.waitAudio(t, "the interrupted turn's audio", 5)

	if !cl.f.StandDown() {
		t.Error("standing down a running turn reported that there was nothing to end")
	}
	cl.waitHeld(t, false, "the ended turn gave the microphone back")
	if cl.f.Listening() {
		t.Error("Listening is true after standing down")
	}

	// The stop has to reach the server. A turn that dropped the microphone without saying stop
	// leaves the recognizer waiting for the rest of a sentence nobody is going to send, and it
	// holds the session's uplink for as long as its own timeout.
	cl.cloud.waitStop(t)

	// And the client is still up, because the next switch back should not have to wait out a
	// reconnect to find out whether the cloud is there.
	if st := cl.f.Status(); st.State != StateConnected {
		t.Errorf("the client is %q after standing down, want it still %q", st.State, StateConnected)
	}
	if *calls != 1 {
		t.Errorf("the other backend was asked %d times, want once: standing down is not taking", *calls)
	}
}

// A switch moved on an idle device. It is the ordinary case — a person moves the switch when
// nothing is happening — and it must be silent: no error for the caller to show, no line saying
// something was cut short when nothing was.
func TestStandDownOnAnIdleDeviceDoesNothing(t *testing.T) {
	cl := connect(t)

	for range 3 {
		if cl.f.StandDown() {
			t.Error("standing down with no turn open reported that something was ended")
		}
	}

	// And the device still works afterwards, which is the thing that would break if standing down
	// had closed the client.
	if err := cl.f.Wake(); err != nil {
		t.Fatalf("a wake word after standing down on an idle device: %v", err)
	}
	cl.waitHeld(t, true, "the turn after an idle stand down is listening")
}

// A wake word with nothing to answer it. The room said the word and the device is silent, and the
// only way anybody finds out why is if the device says: a wake word that does not answer looks
// exactly like a device that did not hear.
func TestAWakeWordSaysWhyWhenThereIsNoTurnToOpen(t *testing.T) {
	cl := connect(t)
	if _, err := cl.term.ask(`{"cmd":"off"}`); err != nil {
		t.Fatalf("off: %v", err)
	}
	waitState(t, cl.f, StateOff)

	err := cl.f.Wake()
	if err == nil {
		t.Fatal("a wake word with the client off opened a turn")
	}
	// The reason has to name the client rather than say "not ready", because the two things that
	// stop a turn here are a client that is off and a client that is still connecting, and they
	// are fixed in different places.
	if !strings.Contains(err.Error(), "off") {
		t.Errorf("the reason was %q, want it to say the client is off", err)
	}
	if cl.f.Listening() {
		t.Error("a refused wake word left a turn open")
	}
}

// The other end of the same requirement: readiness is asked as a question before a turn is attempted,
// so the caller can decide to say something rather than find out from an error string.
func TestBackendReadyNamesWhyItCannotTakeATurn(t *testing.T) {
	cl := connect(t)

	if ready, why := cl.f.BackendReady(); !ready {
		t.Errorf("a connected client is not ready, and says %q", why)
	}

	if _, err := cl.term.ask(`{"cmd":"off"}`); err != nil {
		t.Fatalf("off: %v", err)
	}
	waitState(t, cl.f, StateOff)

	ready, why := cl.f.BackendReady()
	if ready {
		t.Fatal("a client that is off reports itself ready")
	}
	if why == "" {
		t.Error("a client that is off gives no reason")
	}
}

// A wake word said in the middle of a sentence somebody is still saying. The word is part of their
// sentence, not a new request, so the turn that is already open is left alone: opening a second
// listen on one uplink would interleave two recognizers onto one session.
func TestAWakeWordDuringATurnLeavesThatTurnAlone(t *testing.T) {
	cl := connect(t)
	calls := yielded(t)

	if err := cl.f.Wake(); err != nil {
		t.Fatalf("opening the first turn: %v", err)
	}
	cl.waitHeld(t, true, "the first turn is listening")
	cl.speech(t, 20)
	cl.cloud.waitAudio(t, "the first turn's audio", 5)
	_, takesAfterFirst := cl.holding()

	err := cl.f.Wake()
	if err == nil {
		t.Fatal("a wake word over an open turn opened a second one")
	}
	if !strings.Contains(err.Error(), "already") {
		t.Errorf("the reason was %q, want it to say a turn is already open", err)
	}
	if !cl.f.Listening() {
		t.Error("the first turn was ended by the second wake word")
	}
	if _, takes := cl.holding(); takes != takesAfterFirst {
		t.Errorf("the microphone was taken again (%d, was %d): the refused turn claimed it anyway",
			takes, takesAfterFirst)
	}
	if *calls != 1 {
		t.Errorf("the other backend was asked %d times, want once: a refused turn takes nothing", *calls)
	}
}

// A cancel ends the turn and leaves the speaker alone.
//
// The difference from a stop is the speaker, and it is the whole reason Cancel is not Stop: a
// canceled turn was a gesture that turned out to mean something else, so there is no answer to
// silence, and a claim on the speaker for a turn that never spoke is a claim that can displace
// whatever was playing.
func TestCancelEndsTheTurnAndTheMicrophoneComesBack(t *testing.T) {
	cl := connect(t)
	calls := yielded(t)

	if err := cl.f.Wake(); err != nil {
		t.Fatalf("opening a turn to cancel: %v", err)
	}
	cl.waitHeld(t, true, "the turn is running")
	cl.speech(t, 20)
	cl.cloud.waitAudio(t, "the canceled turn's audio", 5)

	cl.f.Cancel()

	cl.waitHeld(t, false, "the canceled turn gave the microphone back")
	if cl.f.Listening() {
		t.Error("Listening is true after a cancel")
	}
	// The utterance is still finished properly, which is the part of ending a turn that is not
	// about the speaker: the server has to be told, or its recognizer waits for a sentence.
	cl.cloud.waitStop(t)
	if *calls != 1 {
		t.Errorf("the other backend was asked %d times, want once", *calls)
	}
}

// A cancel with nothing running is a double tap on a button, which is a person changing their mind.
// It must be a no-op rather than an error: the gesture that reaches it is a second press, and a
// second press is the ordinary way to say "no, not that".
func TestCancelOnAnIdleDeviceDoesNothing(t *testing.T) {
	cl := connect(t)

	cl.f.Cancel()
	cl.f.Cancel()

	if cl.f.Listening() {
		t.Error("canceling with no turn open opened one")
	}
	if held, _ := cl.holding(); held {
		t.Error("canceling with no turn open took the microphone")
	}
}

// The switch moved twenty times, mid-turn included, and the device is still a device that answers.
//
// This is M5's exit test as a unit test. It cannot be the real one — the real one moves a switch on
// a device with both backends, and this can only move the one of them that lives in this package —
// and it is still worth having, because the failure it is looking for is arithmetic rather than
// logic: twenty stand-downs and twenty wake words in a row, each one leaving state behind, and a
// leak shows up as the last one failing.
//
// The midpoint is deliberately in the middle of a turn, which is the case that wedges: the
// microphone is claimed, the turn is abandoned, and the next turn has to be able to claim it again.
func TestTheSwitchCanBeMovedTwentyTimesWithoutWedging(t *testing.T) {
	cl := connect(t)
	calls := yielded(t)

	for i := range 20 {
		if err := cl.f.Wake(); err != nil {
			t.Fatalf("switch %d: opening a turn: %v", i, err)
		}
		cl.waitHeld(t, true, "the turn took the microphone")

		// Half the switches land on an answer being said rather than on silence, because a stop
		// with a speaker claim on it is the other teardown and exercises a different path.
		cl.speech(t, 30)
		cl.cloud.waitAudio(t, "a turn's audio", 10)

		if !cl.f.StandDown() {
			t.Fatalf("switch %d: standing down a running turn reported nothing was ended", i)
		}
		cl.waitHeld(t, false, "the ended turn gave the microphone back")

		// The array is not stuck: it can be taken again straight away, with no sleep, which is the
		// whole claim. A teardown that left a goroutine holding it would pass the check above and
		// fail this one.
		if !cl.f.Listening() && i < 19 {
			if err := cl.f.Wake(); err != nil {
				t.Fatalf("switch %d: reopening immediately after standing down: %v", i, err)
			}
			cl.waitHeld(t, true, "the microphone could be taken again at once")
			cl.f.StandDown()
			cl.waitHeld(t, false, "the second turn gave the microphone back")
		}
	}

	// Twenty switches, each followed by a second turn taken and given back with no pause in
	// between: twenty from the loop and nineteen from the immediate-reopen check, which skips the
	// last because the wake word after the loop is the one that has to prove the device still
	// answers. Counting the handover rather than the turns is the point — it is the thing a second
	// code path could take twice.
	if want := 20 + 19; *calls != want {
		t.Errorf("the other backend was asked %d times, want %d: one per turn, no more", *calls, want)
	}
	if held, takes := cl.holding(); held {
		t.Errorf("the microphone is still held after the last switch (%d takes)", takes)
	}
	// The session survived twenty interruptions of its utterance, which is the thing a server
	// closes a socket over.
	if st := cl.f.Status(); st.State != StateConnected {
		t.Errorf("the client is %q after twenty switches, want still %q", st.State, StateConnected)
	}
}

// Interrupting twenty turns in a row with the gesture path. This is Stop rather than StandDown, and
// the difference is the speaker: Stop reaches for it even with no turn behind, which is what a press
// of the action button means and what a barge-in test written before wake words existed would have
// missed.
//
// No answer is being played here, so there is no speaker claim to interrupt. What is being counted
// is the turn teardown twenty times over — the speaker half of Stop is exercised by the test below
// this one, where there is an answer to cut off.
func TestATurnCanBeInterruptedTwentyTimesWithoutWedging(t *testing.T) {
	cl := connect(t)
	calls := yielded(t)

	for i := range 20 {
		if err := cl.f.Wake(); err != nil {
			t.Fatalf("turn %d: opening a turn: %v", i, err)
		}
		cl.waitHeld(t, true, "the turn took the microphone")
		cl.speech(t, 30)
		cl.cloud.waitAudio(t, "a turn's audio", 10)

		cl.f.Stop()
		cl.waitHeld(t, false, "the interrupted turn gave the microphone back")
	}

	if want := 20; *calls != want {
		t.Errorf("the other backend was asked %d times, want %d: one per turn", *calls, want)
	}
	if held, takes := cl.holding(); held {
		t.Errorf("the microphone is still held after the last interrupt (%d takes)", takes)
	}

	// The device still answers, which is the whole claim of a repeated switch: a teardown that
	// left a goroutine holding the microphone passes every check above and fails this one.
	if err := cl.f.Wake(); err != nil {
		t.Fatalf("a wake word after twenty interrupts: %v", err)
	}
	cl.waitHeld(t, true, "the device still answers after twenty interrupts")
	if st := cl.f.Status(); st.State != StateConnected {
		t.Errorf("the client is %q after twenty interrupts, want still %q", st.State, StateConnected)
	}
}

// A wake word over an answer being said. The order is the test: the speaker has to be free before
// the microphone opens, or the room hears this device talk over its own answer while the recognizer
// is still being set up.
func TestAWakeWordSilencesAnAnswerBeforeItListens(t *testing.T) {
	cl := connect(t)

	// The handover is the thing that would catch the order, so it is the hook that looks. What it
	// records is whether the answer was still being said at the moment the microphone was asked
	// for, which is the state that must be false by then.
	var spokeWhenAsked bool
	old := yielder.fn
	YieldMic(func() {
		if d := cl.f.downlink(); d != nil {
			spokeWhenAsked = d.isSpeaking()
		}
	})
	t.Cleanup(func() { YieldMic(old) })

	// A turn with an answer on the speaker, from a server that really said it. Nothing is stubbed
	// but the card, so this is an answer that arrived over a socket and is being decoded.
	cl.cloud.speak(t, answer(t, defaultDownlinkRate, 60, 25, 440))
	cl.waitSpeaking(t, true, "the server's answer is being played")

	if err := cl.f.Wake(); err != nil {
		t.Fatalf("a wake word over an answer: %v", err)
	}
	cl.waitHeld(t, true, "the interrupting turn took the microphone")

	if spokeWhenAsked {
		t.Error("the microphone was taken while the answer was still on the speaker: the room " +
			"heard this device open its microphone over the top of its own answer")
	}
	if cl.speaking() {
		t.Error("the answer is still playing with the microphone open")
	}
}

// The fix for the wake word landing in its own request's transcript: a turn Wake opens follows the
// word in the audio, throwing away the burst it makes and the gap that ends it, and sends what comes
// after. Nothing before the gap reaches the cloud, and nothing after it is lost.
func TestWakeSkipsTheWakeWord(t *testing.T) {
	cl := connect(t)

	old := wakeTailSkip
	wakeTailSkip = 100 * time.Millisecond // five 20 ms frames
	t.Cleanup(func() { wakeTailSkip = old })

	if err := cl.f.Wake(); err != nil {
		t.Fatalf("opening a wake-triggered turn: %v", err)
	}
	cl.waitHeld(t, true, "a wake-triggered turn is listening")

	// The room first (the gate takes the opening 100 ms as its floor), then a word of ten loud frames, the gap that ends it, then nine frames of request: three packets
	// and no remainder. A test that saw seven packets or more would be saying the word was sent.
	cl.hush(t, 5)
	cl.speech(t, 10)
	cl.hush(t, wakeQuietFrames)
	cl.speech(t, 9)
	cl.cloud.waitAudio(t, "the request's audio", 3)

	_, turn := cl.ask(t, `{"cmd":"listen","state":"stop"}`)
	if turn == nil {
		t.Fatal("stopping the turn did not report the turn it stopped")
	}
	if turn.Frames != 3 {
		t.Errorf("the turn sent %d packets, want 3 — nine frames of request and none of the word", turn.Frames)
	}
	if turn.Pending != 0 {
		t.Errorf("the turn's pending tail is %d samples, want 0", turn.Pending)
	}
}
