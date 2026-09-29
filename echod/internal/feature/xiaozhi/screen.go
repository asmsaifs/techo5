package xiaozhi

import (
	"log/slog"
	"strings"

	"github.com/HuskerMinion/techo5/echod/internal/lib/hook"
)

// State is what a xiaozhi turn looks like from outside: which phase it is in and the words so far.
// It mirrors the voice.State shape so the display can treat them uniformly.
type State struct {
	Phase string // "idle", "listening", "thinking", "replying"
	Heard string // STT text so far
	Reply string // LLM text so far
}

// Changed fires on every phase change and whenever a transcript or a reply arrives.
// It runs on the control socket's goroutine, so listeners must not block.
var Changed hook.Hook[State]

// current is the live state, updated by onEvent.
var current State

// fire emits the current state if anything changed.
func fire() {
	Changed.Emit(current)
}

// onScreenEvent updates the screen state from a server event.
// Called from onEvent on the socket's goroutine.
func onScreenEvent(e Event) {
	changed := false
	switch e.Type {
	case TypeSTT:
		if e.Text != "" && e.Text != current.Heard {
			slog.Info("xiaozhi screen: stt", "text", e.Text)
			current.Heard = e.Text
			current.Phase = "listening"
			changed = true
		}
	case TypeLLM:
		if e.Text != "" {
			// LLM can arrive in chunks; append or replace based on state
			if e.State == "start" || current.Reply == "" {
				current.Reply = e.Text
			} else if e.State == "end" || e.State == "sentence_end" {
				// final or continuation
				if current.Reply != "" && !strings.HasSuffix(current.Reply, e.Text) {
					current.Reply += e.Text
				}
			} else {
				// streaming chunk
				current.Reply += e.Text
			}
			current.Phase = "thinking"
			changed = true
		}
	case TypeTTS:
		if e.State == TTSStart {
			current.Phase = "replying"
			changed = true
		}
	}

	if changed {
		slog.Info("xiaozhi screen: fire", "phase", current.Phase, "heard", current.Heard, "reply", current.Reply)
		fire()
	}
}

// reset clears the state when a turn ends.
func resetScreen() {
	slog.Info("xiaozhi screen: reset to idle")
	current = State{Phase: "idle"}
	fire()
}
