package voice

import (
	"testing"
	"time"
)

func TestOnlyNoise(t *testing.T) {
	for text, want := range map[string]bool{
		" Thank you.": true, "You": true, "so": true, "Thanks for watching!": true, "Yeah.": true,
		"": false, "...": false, "Thank you, turn off the lights.": false, "What time is it?": false,
		"Stop.": false, "Yes": false, "Продолжение следует...": true, "Спасибо, выключи свет": false,
	} {
		if got := onlyNoise(text); got != want {
			t.Errorf("onlyNoise(%q) = %v, want %v", text, got, want)
		}
	}
}

// listen collects what Mistaken is told for the rest of the test.
func listen(t *testing.T) chan Mistake {
	t.Helper()
	got := make(chan Mistake, 4)
	stop := Mistaken.Listen(func(m Mistake) { got <- m })
	t.Cleanup(stop)
	return got
}

// A wake followed by only "Thank you." ends there, and the answer Home Assistant goes on to send is not
// taken up: the device does not say "You're welcome" to an empty room.
func TestAWakeThatHeardOnlyNoiseEndsQuietly(t *testing.T) {
	c := thinkingTurn(t)
	c.woke = true
	got := listen(t)

	c.handle(event{kind: evHeard, text: " Thank you."})
	if c.phase != phaseIdle {
		t.Fatalf("after hearing only noise the turn is %s, want idle", c.phase)
	}
	select {
	case m := <-got:
		if m.Heard != " Thank you." {
			t.Errorf("the mistake says it heard %q", m.Heard)
		}
	case <-time.After(time.Second):
		t.Fatal("the mistake was not told")
	}

	c.handle(event{kind: evReplyText, text: "You're welcome!"})
	if c.shown.Reply != "" {
		t.Errorf("the ended turn took up the reply %q", c.shown.Reply)
	}
}

// Nothing to write down after a wake is a wake by mistake, not a failure: no chime, no red ring.
func TestNothingSaidEndsWithoutTrouble(t *testing.T) {
	c := thinkingTurn(t)
	c.woke = true
	got := listen(t)

	c.handle(event{kind: evError, code: errNoText, msg: "No text recognized"})
	if c.phase != phaseIdle {
		t.Fatalf("after nothing was heard the turn is %s, want idle", c.phase)
	}
	if c.sound.Busy() {
		t.Error("a sound is playing after a turn where nothing was said")
	}
	select {
	case m := <-got:
		if m.Heard != "" {
			t.Errorf("the mistake says it heard %q", m.Heard)
		}
	case <-time.After(time.Second):
		t.Fatal("the mistake was not told")
	}
}

// In a follow-up "Yeah." is an answer, and nothing said is how a follow-up ends: neither is a mistake.
func TestAFollowUpIsNotAMistake(t *testing.T) {
	c := thinkingTurn(t)
	c.followUp = true
	got := listen(t)

	c.handle(event{kind: evHeard, text: "Yeah."})
	if c.phase == phaseIdle {
		t.Fatal("a follow-up answered with \"Yeah.\" was ended")
	}
	c.handle(event{kind: evError, code: errNoText})
	select {
	case m := <-got:
		t.Errorf("a follow-up was told as a mistake: %+v", m)
	case <-time.After(100 * time.Millisecond):
	}
}

// A turn a button or Home Assistant opened is somebody's: "Yeah." answers the question Home Assistant
// asked, and nothing said is not the wake word having misheard.
func TestATurnNotOpenedByTheWakeWordIsNotAMistake(t *testing.T) {
	c := thinkingTurn(t)
	got := listen(t)

	c.handle(event{kind: evHeard, text: "Yeah."})
	if c.phase == phaseIdle {
		t.Fatal("an answer to Home Assistant's question was ended as noise")
	}
	c.handle(event{kind: evError, code: errNoText})
	select {
	case m := <-got:
		t.Errorf("a turn the wake word did not open was told as a mistake: %+v", m)
	case <-time.After(100 * time.Millisecond):
	}
}

// Home Assistant goes on with a run ended as a mistake, and its last events are still to come. A wake
// in the meantime waits for that run to close rather than opening into its leftovers.
func TestAWakeWaitsForAMistakenRunToClose(t *testing.T) {
	c := thinkingTurn(t)
	c.woke = true
	listen(t)

	c.handle(event{kind: evHeard, text: "Thank you."})
	if !c.draining || c.grace == nil {
		t.Fatalf("after a mistaken turn: draining %v, grace %v", c.draining, c.grace != nil)
	}
	asked := time.Now().Add(-time.Second)
	c.handle(event{kind: evStart, slot: 0, woke: true, at: asked})
	if c.phase != phaseIdle || c.pending == nil || !c.pending.woke || !c.pending.at.Equal(asked) {
		t.Fatalf("a wake while draining: phase %s, pending %+v", c.phase, c.pending)
	}

	c.handle(event{kind: evReplyText, text: "You're welcome!"})
	if c.shown.Reply != "" {
		t.Errorf("the mistaken run's reply was taken up: %q", c.shown.Reply)
	}

	c.handle(event{kind: evRunEnd})
	if c.draining || c.pending != nil {
		t.Errorf("after the run closed: draining %v, pending %+v", c.draining, c.pending)
	}
}
