package xiaozhi

import (
	"testing"
	"time"
)

// The cloud closes a session with no turn in it after about a minute. That is not a failing endpoint:
// the next attempt is at once and the backoff starts over.
func TestNextDelayAfterAHealthySession(t *testing.T) {
	delay, next := nextDelay(40*time.Second, 60*time.Second)
	if delay != 0 || next != retryInitial {
		t.Fatalf("a minute-long session gave delay %v and backoff %v, want none and %v", delay, next, retryInitial)
	}
	delay, next = nextDelay(retryInitial, healthySession)
	if delay != 0 {
		t.Fatalf("a session exactly healthySession long waited %v", delay)
	}
}

// A connection that never came up, or fell over at once, is a failure: the wait is what it was and the
// next one is twice it, up to the cap.
func TestNextDelayAfterAFailure(t *testing.T) {
	delay, next := nextDelay(retryInitial, 0)
	if delay != retryInitial || next != 2*retryInitial {
		t.Fatalf("a failed attempt gave delay %v and backoff %v", delay, next)
	}
	delay, next = nextDelay(retryInitial, healthySession-time.Second)
	if delay != retryInitial {
		t.Fatalf("a session just under healthySession waited %v, want the backoff", delay)
	}
	if _, next = nextDelay(retryMax, time.Second); next != retryMax {
		t.Fatalf("the backoff went past its cap: %v", next)
	}
}

// A wake word that lands between two sessions waits for the one being opened.
func TestReadyWithinWaitsForAReconnect(t *testing.T) {
	f := build()
	f.set(Status{State: StateDisconnected})
	go func() {
		time.Sleep(120 * time.Millisecond)
		f.set(Status{State: StateConnected})
	}()
	start := time.Now()
	if ready, why := f.readyWithin(2 * time.Second); !ready {
		t.Fatalf("not ready after a reconnect: %s", why)
	}
	if took := time.Since(start); took > time.Second {
		t.Fatalf("waited %v for a session that was up after 120 ms", took)
	}
	select {
	case <-f.wake:
	default:
		t.Fatal("the wait did not nudge the reconnect loop")
	}
}

func TestReadyWithinGivesUp(t *testing.T) {
	f := build()
	f.set(Status{State: StateDisconnected})
	start := time.Now()
	ready, why := f.readyWithin(200 * time.Millisecond)
	if ready || why == "" {
		t.Fatalf("ready=%v why=%q with no session coming", ready, why)
	}
	if took := time.Since(start); took < 150*time.Millisecond {
		t.Fatalf("gave up after %v, before its grace ran out", took)
	}
}

// A client that is off, or waiting for an activation code, is not going to mend in a few seconds.
func TestReadyWithinDoesNotWaitWhenOff(t *testing.T) {
	for _, state := range []string{StateOff, StateActivating} {
		f := build()
		f.set(Status{State: state})
		start := time.Now()
		if ready, _ := f.readyWithin(2 * time.Second); ready {
			t.Fatalf("%s: ready", state)
		}
		if took := time.Since(start); took > 100*time.Millisecond {
			t.Fatalf("%s: waited %v", state, took)
		}
	}
}
