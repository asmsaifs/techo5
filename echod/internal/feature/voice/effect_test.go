package voice

import (
	"testing"
	"time"

	"github.com/HuskerMinion/techo5/echod/internal/component"
	"github.com/HuskerMinion/techo5/echod/internal/config"
	"github.com/HuskerMinion/techo5/echod/internal/feature/wakeword"
	"github.com/HuskerMinion/techo5/echod/internal/hardware/led"
)

// handleNext handles what the turn was posted, failing if nothing was.
func handleNext(t *testing.T, c *conversation) {
	t.Helper()
	select {
	case e := <-c.events:
		c.handle(e)
	case <-time.After(time.Second):
		t.Fatal("nothing was posted to the turn")
	}
}

// An effect asked for during a request shows at once on the step it is in; None clears it, Default
// puts the step's own back, and a name the ring does not have is refused.
func TestAnEffectAskedForShowsOnTheStep(t *testing.T) {
	c := thinkingTurn(t)
	v := &Voice{turn: c}
	own := c.claim.Showing()
	var other string
	for _, name := range led.EffectNames() {
		if name != own.Effect {
			other = name
			break
		}
	}

	if err := v.ShowEffect("Not an effect"); err == nil {
		t.Error("an effect the ring does not have was taken")
	}

	if err := v.ShowEffect(other); err != nil {
		t.Fatal(err)
	}
	handleNext(t, c)
	if got := c.claim.Showing(); got.Effect != other {
		t.Errorf("the thinking request shows %q, want %q", got.Effect, other)
	}

	if err := v.ShowEffect(component.EffectNone); err != nil {
		t.Fatal(err)
	}
	handleNext(t, c)
	if got := c.claim.Showing(); got.Effect != "" {
		t.Errorf("None left %q on the ring", got.Effect)
	}

	if err := v.ShowEffect(component.EffectDefault); err != nil {
		t.Fatal(err)
	}
	handleNext(t, c)
	if got := c.claim.Showing(); got.Effect != own.Effect || got.Reverse != own.Reverse {
		t.Errorf("Default shows %q reversed %v, want the step's own %q reversed %v",
			got.Effect, got.Reverse, own.Effect, own.Reverse)
	}
}

// With no request running, an effect asked for is not an error and changes nothing.
func TestAnEffectWithNoRequestDoesNothing(t *testing.T) {
	c := thinkingTurn(t)
	c.handle(event{kind: evCancel})
	v := &Voice{turn: c}
	for len(c.events) > 0 {
		<-c.events
	}

	if err := v.ShowEffect(led.EffectNames()[0]); err != nil {
		t.Errorf("refused with no request running: %v", err)
	}
	select {
	case e := <-c.events:
		t.Errorf("posted %v with no request running", e.kind)
	case <-time.After(50 * time.Millisecond):
	}
}

// With the wake word's own effect None and no step overrides, a request shows nothing; an effect an
// automation put up goes again when the request moves on, and Default takes it off.
func TestAnEffectGoesWithItsStepWhenTheWakeWordHasNone(t *testing.T) {
	c := thinkingTurn(t)
	if err := config.Set().Wake(c.slot).Effect(component.EffectNone); err != nil {
		t.Fatal(err)
	}
	c.showPhase(wakeword.ThinkingEffect(c.slot))
	if got := c.claim.Showing(); got.Effect != "" {
		t.Fatalf("a request with no effects shows %q", got.Effect)
	}
	v := &Voice{turn: c}
	name := led.EffectNames()[0]

	if err := v.ShowEffect(name); err != nil {
		t.Fatal(err)
	}
	handleNext(t, c)
	if got := c.claim.Showing(); got.Effect != name {
		t.Fatalf("the thinking request shows %q, want %q", got.Effect, name)
	}
	if err := v.ShowEffect(component.EffectDefault); err != nil {
		t.Fatal(err)
	}
	handleNext(t, c)
	if got := c.claim.Showing(); got.Effect != "" {
		t.Errorf("Default left %q on the ring", got.Effect)
	}

	if err := v.ShowEffect(name); err != nil {
		t.Fatal(err)
	}
	handleNext(t, c)
	c.showPhase(wakeword.ReplyingEffect(c.slot)) // as speak does
	if got := c.claim.Showing(); got.Effect != "" {
		t.Errorf("the reply kept %q from the step before", got.Effect)
	}
}

// An automation asking for effects faster than the turn draws them has one event in the queue at a
// time, and the turn draws the latest.
func TestEffectsAskedForQuicklyTakeOneEvent(t *testing.T) {
	c := thinkingTurn(t)
	v := &Voice{turn: c}
	names := led.EffectNames()
	before := len(c.events)
	for i := 0; i < 100; i++ {
		if err := v.ShowEffect(names[i%len(names)]); err != nil {
			t.Fatal(err)
		}
	}
	if n := len(c.events) - before; n != 1 {
		t.Fatalf("%d events queued, want 1", n)
	}
	handleNext(t, c)
	if got, want := c.claim.Showing().Effect, names[99%len(names)]; got != want {
		t.Errorf("the ring shows %q, want the latest asked for, %q", got, want)
	}
	if err := v.ShowEffect(component.EffectNone); err != nil {
		t.Fatal(err)
	}
	handleNext(t, c)
	if got := c.claim.Showing().Effect; got != "" {
		t.Errorf("None after the burst left %q", got)
	}
}
