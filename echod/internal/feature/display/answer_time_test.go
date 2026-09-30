//go:build !dot

package display

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/HuskerMinion/techo5/echod/internal/config"
	"github.com/HuskerMinion/techo5/echod/internal/feature/voice"
)

// Each Answer time choice keeps a finished turn's words up for as long as it says; nothing chosen is
// five seconds, and a value no choice has is five seconds too.
func TestAnswerTime(t *testing.T) {
	config.Use(filepath.Join(t.TempDir(), "state.json"))
	if got := linger(); got != 5*time.Second {
		t.Fatalf("default %v", got)
	}
	s := answerTimeSelect()
	for label, want := range map[string]time.Duration{
		"10 seconds":   10 * time.Second,
		"20 seconds":   20 * time.Second,
		"Until tapped": untilTapped,
		"5 seconds":    5 * time.Second,
	} {
		s.OnCommand(label)
		if got := linger(); got != want {
			t.Errorf("%s: %v, want %v", label, got, want)
		}
	}
	if err := config.Set().Screen().AnswerSeconds(7); err != nil {
		t.Fatal(err)
	}
	if got := linger(); got != 5*time.Second {
		t.Errorf("an unknown value: %v", got)
	}
}

// A tap on a finished turn's words puts them away, and they are away until the next turn; a turn still
// going, or words already past their time, are not an answer to clear.
func TestAnswerClearedByTap(t *testing.T) {
	config.Use(filepath.Join(t.TempDir(), "state.json"))
	d := &Display{poke: make(chan struct{}, 1)}
	now := time.Now()

	d.view, d.viewAt = voice.State{Phase: "idle", Heard: "what time is it", Reply: "It's noon."}, now
	if !d.answerUp(now) {
		t.Fatal("a fresh answer is not up")
	}
	d.clearAnswer()
	if d.answerUp(now) {
		t.Error("the answer is still up after a tap")
	}

	d.quiet = false
	if d.answerUp(now.Add(6 * time.Second)) {
		t.Error("an answer past its five seconds is still up")
	}
	d.view.Phase = "replying"
	if d.answerUp(now) {
		t.Error("a turn still replying counts as a finished answer")
	}
}
