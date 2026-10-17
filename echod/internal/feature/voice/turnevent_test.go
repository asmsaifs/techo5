package voice

import (
	"testing"
	"time"

	"github.com/HuskerMinion/techo5/echod/internal/component"
	"github.com/HuskerMinion/techo5/echod/internal/config"
	"github.com/HuskerMinion/techo5/echod/internal/feature/activity"
)

// turnEvents collects what the turns fire for the rest of the test.
func turnEvents(t *testing.T) chan component.Event {
	t.Helper()
	got := make(chan component.Event, 16)
	stop := component.Fire.Listen(func(e component.Event) {
		if e.Name == activity.TurnEvent {
			got <- e
		}
	})
	t.Cleanup(stop)
	return got
}

// ended waits for the event of the turn with id. It goes out from a goroutine of its own, and another
// test's turn can still be on its way, so it is picked out by id.
func ended(t *testing.T, got chan component.Event, id string) map[string]string {
	t.Helper()
	end := time.After(2 * time.Second)
	for {
		select {
		case e := <-got:
			if e.Data["id"] == id {
				return e.Data
			}
		case <-end:
			t.Fatalf("no event for turn %s", id)
		}
	}
}

// Every way a turn ends reports one event, with what opened it and how it went.
func TestEveryEndingFiresItsEvent(t *testing.T) {
	for _, tc := range []struct {
		name    string
		start   event
		then    []event
		ended   string
		trigger string
		error   string
		heard   string
	}{
		{
			name:  "answered",
			start: event{kind: evStart, woke: true},
			then: []event{
				{kind: evHeard, text: "what time is it"},
				{kind: evReplyText, text: "It's noon."},
				{kind: evRunEnd},
			},
			ended: "answered", trigger: "wake_word", heard: "what time is it",
		},
		{
			name:  "error",
			start: event{kind: evStart},
			then:  []event{{kind: evError, code: "stt-stream-failed", msg: "no audio"}},
			ended: "error", trigger: "button", error: "stt-stream-failed",
		},
		{
			name:  "timeout",
			start: event{kind: evStart},
			then:  []event{{kind: evTimeout}},
			ended: "error", trigger: "button", error: "timeout",
		},
		{
			name:  "canceled",
			start: event{kind: evStart, asked: true},
			then:  []event{{kind: evCancel}},
			ended: "canceled", trigger: "home_assistant",
		},
		{
			name:  "nothing said",
			start: event{kind: evStart, woke: true},
			then:  []event{{kind: evError, code: errNoText}},
			ended: "no_speech", trigger: "wake_word",
		},
		{
			name:  "only noise",
			start: event{kind: evStart, woke: true},
			then:  []event{{kind: evHeard, text: "Thank you."}},
			ended: "no_speech", trigger: "wake_word", heard: "Thank you.",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			w := newWatcher(t, config.OnHA{})
			got := turnEvents(t)

			tc.start.at = w.at
			w.handle(tc.start)
			if w.phase != phaseListening || w.turn == nil {
				t.Fatalf("the turn did not open: phase %s", w.phase)
			}
			id := w.turn.ID()
			for _, e := range tc.then {
				w.handle(e)
			}
			if w.phase != phaseIdle {
				t.Fatalf("the turn is %s, want ended", w.phase)
			}

			data := ended(t, got, id)
			if data["ended"] != tc.ended || data["trigger"] != tc.trigger {
				t.Errorf("ended %q by %q, want %q by %q", data["ended"], data["trigger"], tc.ended, tc.trigger)
			}
			if data["error"] != tc.error {
				t.Errorf("error is %q, want %q", data["error"], tc.error)
			}
			if data["heard"] != tc.heard {
				t.Errorf("heard is %q, want %q", data["heard"], tc.heard)
			}
			for key := range data {
				if key == "audio" || key == "pcm" {
					t.Errorf("the event carries %s", key)
				}
			}
		})
	}
}

func TestWhatOpenedATurn(t *testing.T) {
	for n, want := range map[nextTurn]activity.Trigger{
		{woke: true}:                  activity.WakeWord,
		{woke: true, continued: true}: activity.WakeWord,
		{followUp: true}:              activity.FollowUp,
		{asked: true}:                 activity.HomeAssistant,
		{}:                            activity.Button,
	} {
		if got := n.trigger(); got != want {
			t.Errorf("%+v opened by %q, want %q", n, got, want)
		}
	}
}
