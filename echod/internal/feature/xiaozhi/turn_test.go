package xiaozhi

// A turn, end to end: the microphone is subscribed, the frames go out, the turn ends, and the
// microphone is let go.
//
// Every one of those is a thing that can be got wrong in a way nothing else would notice. A turn
// that forgot its unlisten would hold the array open for good and the device would go deaf with
// the switch still on; a turn that sent its stop before its audio would cut the recognizer off
// mid-word; a second turn opened over the first would interleave two conversations onto one
// session. So the checks are on what came out in what order, and on the microphone going back.

import (
	"context"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/HuskerMinion/techo5/echod/internal/config"
	"github.com/HuskerMinion/techo5/echod/internal/hardware/mic"
)

// manualMode is a turn the device ends rather than the server. The client has no constant for it
// on purpose — auto is the only mode it names, and anything else means this — so a test that
// wanted a device-ended turn has to name it itself, which is the clearest statement of the
// difference there is.
const manualMode = "manual"

// client is a running client with the switch on and a session open, and the two ways of reaching
// it: a terminal on the control socket, and the channel the microphone would be filling.
type client struct {
	f     *Feature
	term  *terminal
	cloud *cloud

	// frames stands in for the array. Fed by the test, drained by the uplink.
	frames chan []int16

	// once closes frames. Shared with the subscription's own teardown, which closes the same
	// channel, so a test that hangs the microphone up and a turn that ends do not both.
	once sync.Once

	// mu guards the two fields below, which the subscription sets from its own goroutine.
	mu    sync.Mutex
	held  bool
	takes int
	names []string
}

// connect stands a client up against a fake cloud and turns the switch on.
func connect(t *testing.T) *client {
	t.Helper()

	restore(t)

	// Off by default, so a test counting packets or frames is not also chasing wakeTailSkip's
	// milliseconds. TestWakeSkipsTheTailOfTheWakeWord in wake_test.go sets it back to measure the
	// skip itself.
	oldSkip := wakeTailSkip
	wakeTailSkip = 0
	t.Cleanup(func() { wakeTailSkip = oldSkip })

	c := &cloud{gone: make(chan struct{})}
	c.serve(t)

	// The host is the one setting a self-hosted server needs, and the only one this changes: the
	// codec row is fixed at what was measured, and a test that moved it would be testing a
	// configuration no device holds.
	if err := config.Set().Xiaozhi().Host(c.url); err != nil {
		t.Fatalf("pointing the client at the test cloud: %v", err)
	}

	cl := &client{cloud: c, frames: make(chan []int16, 256)}
	oldMic, oldPath := micListen, ControlPath
	micListen = func(name string) (<-chan []int16, func()) {
		cl.mu.Lock()
		cl.held, cl.takes = true, cl.takes+1
		cl.names = append(cl.names, name)
		cl.mu.Unlock()

		return cl.frames, func() {
			cl.mu.Lock()
			cl.held = false
			cl.mu.Unlock()
		}
	}

	f := build()
	f.control.x = f
	ControlPath = shortSocket(t)

	ctx, cancel := context.WithCancel(context.Background())
	run := make(chan error, 1)
	go func() { run <- f.Run(ctx) }()

	// One teardown, in one order. Run coming back is not the client being down: the control socket
	// is removed by a goroutine of its own, after Run has returned, and the paths are read by name.
	// So the file has to be gone before either global is put back, or the next test's socket is
	// named by a path that is being changed under the one removing it.
	t.Cleanup(func() {
		cl.hangUp()
		cancel()
		select {
		case err := <-run:
			if err != nil {
				t.Errorf("Run: %v", err)
			}
		case <-time.After(5 * time.Second):
			t.Error("Run did not come back after its context ended")
		}
		waitGone(t, ControlPath)
		ControlPath, micListen = oldPath, oldMic
	})

	waitSocket(t, ControlPath)
	cl.f, cl.term = f, newTerminal(t, ControlPath)
	if _, err := cl.term.ask(`{"cmd":"on"}`); err != nil {
		t.Fatalf("on: %v", err)
	}
	waitState(t, f, StateConnected)
	return cl
}

// waitGone waits for a path to be taken away, which is how the control server says it has stopped.
func waitGone(t *testing.T, path string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(path); os.IsNotExist(err) {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Errorf("the control socket at %s is still there", path)
}

// hangUp is the array being let go under the uplink, and once because the subscription's teardown
// closes the same channel.
func (c *client) hangUp() { c.once.Do(func() { close(c.frames) }) }

// say hands the uplink a frame of the microphone.
//
// The wait is the point. A test that pushed as fast as it could and then looked would be racing
// the goroutine it is testing, and would pass or fail for whichever order the scheduler picked.
func (c *client) say(t *testing.T, frame []int16) {
	t.Helper()
	select {
	case c.frames <- frame:
	case <-time.After(5 * time.Second):
		t.Fatal("the uplink did not take a microphone frame")
	}
}

// speech is a turn's worth of a voice: loud enough for the endpoint to take it for speech.
func (c *client) speech(t *testing.T, frames int) {
	t.Helper()
	for range frames {
		c.say(t, tone(mic.FrameSamples))
	}
}

// hush is the silence that ends one.
func (c *client) hush(t *testing.T, frames int) {
	t.Helper()
	for range frames {
		c.say(t, make([]int16, mic.FrameSamples))
	}
}

// ask is a command and the goodbye that answers it, with the turn it opened.
//
// ask itself cannot be used here: it returns the first status it reads, and opening a turn
// publishes one on the way through. The socket is emptied first, because a turn broadcasts as it
// opens and as it closes, and a goodbye left over from the last one would be taken for the answer
// to this — a terminal watching two turns would otherwise see the first one open twice.
func (c *client) ask(t *testing.T, line string) (reply, *Turn) {
	t.Helper()
	c.term.drain()
	if err := c.term.write(line); err != nil {
		t.Fatalf("%s: %v", line, err)
	}
	for {
		r, err := c.term.next()
		if err != nil {
			t.Fatalf("%s: %v", line, err)
		}
		switch r.Event {
		case eventError, eventGoodbye:
			return r, r.Turn
		case eventTurn:
			if r.Turn != nil && r.Turn.State != "" {
				return r, r.Turn
			}
		}
	}
}

// refuse is a command expected to be turned down, with the sentence that says why.
func (c *client) refuse(t *testing.T, line string) string {
	t.Helper()
	r, _ := c.ask(t, line)
	if r.Event != eventError || r.Err == "" {
		t.Fatalf("%s answered %+v, want an error that says why", line, r)
	}
	return r.Err
}

// end is a turn finishing on its own, matched by id so that one a later turn opened is not
// mistaken for it. A terminal is left holding the other replies the turn produced.
func (c *client) end(t *testing.T, id int) *Turn {
	t.Helper()
	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		r, err := c.term.next()
		if err != nil {
			t.Fatalf("waiting for turn %d to end: %v", id, err)
		}
		if r.Event == eventTurn && r.Turn != nil && r.Turn.ID == id && r.Turn.State != "" {
			return r.Turn
		}
	}
	t.Fatalf("turn %d never ended", id)
	return nil
}

// speaking is whether an answer is on the speaker right now, and false rather than a failure when
// there is no downlink at all, which is the ordinary state before a session has been built.
func (c *client) speaking() bool {
	d := c.f.downlink()
	return d != nil && d.isSpeaking()
}

// waitSpeaking waits for an answer to start or stop, for a check about the speaker. The cloud's
// writes and the device's read loop are both other goroutines, so a test that looked straight after
// writing would be looking at a write.
func (c *client) waitSpeaking(t *testing.T, want bool, why string) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if c.speaking() == want {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("the device is speaking %v after 10s, want %v: %s", c.speaking(), want, why)
}

// holding is whether the microphone is still subscribed, and how many times it has been taken.
func (c *client) holding() (bool, int) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.held, c.takes
}

// waitHeld waits for the microphone to be subscribed or given back. A turn answers before its
// uplink goroutine has run, so a test that looked straight afterwards would be reading a race
// rather than a state.
func (c *client) waitHeld(t *testing.T, want bool, why string) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if held, _ := c.holding(); held == want {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	held, takes := c.holding()
	t.Fatalf("the microphone is %v after %d takes, want %v: %s", held, takes, want, why)
}

// spoken is the JSON the server was sent, decoded and in order. Undecodable lines are reported
// through t by the caller that reads them, so this stays a plain read of the record.
func (c *client) spoken(t *testing.T) []Event { return c.cloud.messages(t) }

// The one turn that matters end to end: the array is subscribed by name, the frames go out as
// binary, the stop follows the audio rather than preceding it, and the array is given back.
func TestATurnSendsAudioThenStopsAndLetsTheMicrophoneGo(t *testing.T) {
	cl := connect(t)

	r, turn := cl.ask(t, `{"cmd":"listen","state":"start","mode":"auto"}`)
	if turn == nil {
		t.Fatalf("listening answered %+v, want the turn that was opened", r)
	}
	if turn.Mode != ListenAuto {
		t.Errorf("the turn is in %q mode, want %q", turn.Mode, ListenAuto)
	}
	cl.waitHeld(t, true, "a turn that answered is running")
	if _, takes := cl.holding(); takes != 1 {
		t.Fatalf("the microphone was taken %d times, want once", takes)
	}

	// 600 ms is ten whole packets at 60 ms, which is what a turn of a sentence or two looks like.
	cl.speech(t, 30)
	cl.cloud.waitAudio(t, "a turn's audio", 10)

	if _, err := cl.term.ask(`{"cmd":"listen","state":"stop"}`); err != nil {
		t.Fatalf("stop: %v", err)
	}

	// The microphone first: a turn that ended but kept the array would leave the device deaf with
	// the switch still on, and nothing above would have shown it.
	cl.waitHeld(t, false, "a stopped turn gave the microphone back")

	// And the order on the wire. The stop has to be after the audio: a server that hears the stop
	// first throws away what is behind it, and the recognizer loses the last word of every
	// sentence.
	// After the server has seen all three, not when they were written: a write that has returned
	// is on the wire and not yet read, and this is a claim about what the server saw.
	var heard int
	var after int
	for _, e := range cl.cloud.waitText(t, "a start, some audio and a stop", 2) {
		switch {
		case e.Type == TypeListen && e.State == "start":
			heard++
		case e.Type == TypeListen && e.State == "stop":
			after = len(cl.cloud.packets())
			heard++
		}
	}
	if heard != 2 {
		t.Errorf("the server heard %d listen messages, want a start and a stop: %v",
			heard, kinds(cl.cloud.waitText(t, "a start and a stop", 2)))
	}
	if after != 10 {
		t.Errorf("the server had %d audio frames when the stop arrived, want 10: the stop is "+
			"ahead of the audio it is meant to end", after)
	}
}

// A turn the device ends by itself, with no stop command behind it. This is the path M2 exists
// for: the server says when the speaker finished, and the client works that out and says so.
func TestATurnTheDeviceEndsItselfOnSilence(t *testing.T) {
	cl := connect(t)

	r, turn := cl.ask(t, `{"cmd":"listen","state":"start","mode":"`+manualMode+`"}`)
	if turn == nil {
		t.Fatalf("listening answered %+v, want the turn that was opened", r)
	}
	// Not a constant, because "manual" is not a mode the client has: auto is the only one it
	// names, and anything else means the device ends the utterance. So this checks the round trip
	// — what was asked for is what the server was told — rather than a value of its own.
	if turn.Mode != manualMode {
		t.Errorf("the turn is in %q mode, want %q", turn.Mode, manualMode)
	}

	// Half a second of speech and then seven tenths of a second of nothing. The endpoint needs
	// three loud windows to believe anybody is speaking and six quiet ones to stop believing it,
	// so this is the shortest turn it can end on its own.
	cl.speech(t, 25)
	cl.hush(t, 35)

	ended := cl.end(t, turn.ID)
	if !strings.Contains(ended.State, "finished") {
		t.Errorf("the turn ended %q, want the speaker having finished", ended.State)
	}
	// In seconds, and about the half second of speech there was. It is a window of the endpoint's
	// rather than a sample count, so it lands on 0.5 and not on 0: the frames past the end of the
	// speech are already on the wire and this is where the turn should have stopped.
	if ended.EndSpeech < 0.4 || ended.EndSpeech > 0.8 {
		t.Errorf("the turn was cut at %.2f s, want about the 0.5 s of speech it was given", ended.EndSpeech)
	}
	if ended.Frames < 8 {
		t.Errorf("the turn sent %d frames, want the eight or so half a second of speech is", ended.Frames)
	}

	cl.waitHeld(t, false, "a turn the device ended itself gave the microphone back")
	if msg := cl.cloud.waitText(t, "a start and a stop", 2); !stopped(msg) {
		t.Errorf("the server was never told the turn ended: %v", kinds(msg))
	}
}

// The microphone going away is not a turn that ends: the device is gone, and the session is about
// to be dropped. It still has to let the array go, or a later turn would find it taken.
func TestALostMicrophoneEndsTheTurn(t *testing.T) {
	cl := connect(t)

	if _, turn := cl.ask(t, `{"cmd":"listen","state":"start","mode":"auto"}`); turn == nil {
		t.Fatal("the turn was not opened")
	}
	cl.speech(t, 5)

	cl.hangUp()
	cl.waitHeld(t, false, "the uplink let the microphone go when it was taken away")
	if _, again := cl.ask(t, `{"cmd":"listen","state":"start","mode":"auto"}`); again == nil {
		t.Error("a turn was left open after the microphone went away")
	}
}

// One turn at a time. Two conversations interleaved onto one session is not a thing the server
// can be asked to untangle, so the second is refused with a sentence rather than started.
func TestOnlyOneTurnAtATime(t *testing.T) {
	cl := connect(t)

	_, first := cl.ask(t, `{"cmd":"listen","state":"start","mode":"auto"}`)
	if msg := cl.refuse(t, `{"cmd":"listen","state":"start","mode":"auto"}`); !strings.Contains(msg, "already") {
		t.Errorf("a second turn was refused with %q, want a sentence saying one is already open", msg)
	}
	if held, takes := cl.holding(); !held || takes != 1 {
		t.Errorf("the microphone was taken %d times, want once: the refused turn opened a second", takes)
	}

	// And stopping the first lets the next one in, so a refusal is not the switch being off.
	if _, err := cl.term.ask(`{"cmd":"listen","state":"stop"}`); err != nil {
		t.Fatalf("stop: %v", err)
	}
	_, second := cl.ask(t, `{"cmd":"listen","state":"start","mode":"auto"}`)
	if second.ID == first.ID {
		t.Errorf("the second turn is also numbered %d, so an answer about one can be read as "+
			"an answer about the other", second.ID)
	}
	cl.waitHeld(t, true, "the second turn took the microphone back")
	if _, takes := cl.holding(); takes != 2 {
		t.Errorf("the microphone was taken %d times, want twice: one per turn", takes)
	}
}

// Listening with no session is the state a wake word finds in, and it has to be refused rather
// than sent into a socket that is not there.
func TestListeningWithNoSessionIsRefused(t *testing.T) {
	restore(t)
	cl := connect(t)

	if _, err := cl.term.ask(`{"cmd":"off"}`); err != nil {
		t.Fatalf("off: %v", err)
	}
	waitState(t, cl.f, StateOff)
	if msg := cl.refuse(t, `{"cmd":"listen","state":"start","mode":"auto"}`); !strings.Contains(msg, "no xiaozhi session") {
		t.Errorf("listening with no session was refused with %q, want a sentence saying there is "+
			"no session to send it on", msg)
	}
	if held, takes := cl.holding(); held || takes != 0 {
		t.Errorf("the microphone was taken %d times and is held %v, want never taken", takes, held)
	}
}

// An abort is the operator pulling the conversation, so the uplink stops first: the stop has to
// reach the server before the abort, or it is discarded along with the turn and the server is left
// with a listen it never heard the end of.
func TestAnAbortStopsTheUplinkFirst(t *testing.T) {
	cl := connect(t)

	if _, turn := cl.ask(t, `{"cmd":"listen","state":"start","mode":"auto"}`); turn == nil {
		t.Fatal("the turn was not opened")
	}
	cl.speech(t, 10)
	cl.cloud.waitAudio(t, "a turn's audio", 3)

	if _, err := cl.term.ask(`{"cmd":"abort"}`); err != nil {
		t.Fatalf("abort: %v", err)
	}
	cl.waitHeld(t, false, "an abort stopped the turn")

	// The order is read off the server's own record, which is the only place it is decided, and
	// only once it has all of it: a stop that has been written but not read would look like it was
	// never sent at all.
	msgs := cl.cloud.waitText(t, "a start, a stop and an abort", 3)
	stop, abort := -1, -1
	for i, e := range msgs {
		if e.Type == TypeListen && e.State == "stop" && stop < 0 {
			stop = i
		}
		if e.Type == TypeAbort && abort < 0 {
			abort = i
		}
	}
	if stop < 0 || abort < 0 {
		t.Fatalf("the server heard %v, want a listen stop and an abort", kinds(msgs))
	}
	if stop > abort {
		t.Errorf("the abort was message %d and the stop %d: the stop has to be first or the "+
			"server discards it with the turn", abort, stop)
	}
}

// Turning the switch off mid-turn is the same as dropping the session under it, and the
// microphone still has to come back. A device that goes deaf after being switched off cannot be
// woken again until it is restarted.
func TestSwitchingOffMidTurnLetsTheMicrophoneGo(t *testing.T) {
	cl := connect(t)

	if _, turn := cl.ask(t, `{"cmd":"listen","state":"start","mode":"auto"}`); turn == nil {
		t.Fatal("the turn was not opened")
	}
	cl.speech(t, 5)

	if _, err := cl.term.ask(`{"cmd":"off"}`); err != nil {
		t.Fatalf("off: %v", err)
	}
	cl.waitHeld(t, false, "switching off let the microphone go")
	// The status, not another turn: there is no session left to open one on, and a refusal would
	// say nothing about whether one is still holding the microphone.
	cl.term.drain()
	r, err := cl.term.ask(`{"cmd":"status"}`)
	if err != nil {
		t.Fatalf("status: %v", err)
	}
	if r.Status.Listening {
		t.Error("the status still says it is listening after the switch went off")
	}
}

// The turn as the status reports it: enough to answer "is it listening and what did the last turn
// cost", which is the whole of what an operator looks at.
func TestStatusCarriesTheUplink(t *testing.T) {
	cl := connect(t)

	if _, turn := cl.ask(t, `{"cmd":"listen","state":"start","mode":"auto"}`); turn == nil {
		t.Fatal("the turn was not opened")
	}
	cl.speech(t, 30)
	cl.cloud.waitAudio(t, "a turn's audio", 10)
	if _, err := cl.term.ask(`{"cmd":"listen","state":"stop"}`); err != nil {
		t.Fatalf("stop: %v", err)
	}
	cl.waitHeld(t, false, "the stopped turn gave the microphone back")
	cl.term.drain()

	r, err := cl.term.ask(`{"cmd":"status"}`)
	if err != nil {
		t.Fatalf("status: %v", err)
	}
	if r.Status.Listening {
		t.Error("the status says it is still listening after the turn was stopped")
	}
	if got := r.Status.Stats.Sent; got != 10 {
		t.Errorf("the status counts %d packets sent, want 10", got)
	}
	if got, want := r.Status.Stats.SentBytes, len(cl.cloud.packets()[0])*10; got != want {
		t.Errorf("the status counts %d bytes sent, want %d", got, want)
	}
}

// stopped is whether the server was told a turn ended.
func stopped(msgs []Event) bool {
	for _, e := range msgs {
		if e.Type == TypeListen && e.State == "stop" {
			return true
		}
	}
	return false
}

// kinds is the messages as type and state, for a failure that has to say what arrived instead of
// what was wanted.
func kinds(msgs []Event) []string {
	var out []string
	for _, e := range msgs {
		if e.State != "" {
			out = append(out, e.Type+" "+e.State)
			continue
		}
		out = append(out, e.Type)
	}
	return out
}
