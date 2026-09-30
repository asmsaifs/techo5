package xiaozhi

import (
	"log/slog"
	"strings"

	"github.com/HuskerMinion/techo5/echod/internal/config"
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
			current.Reply = "" // a new request; the last answer is not this one's
			current.Phase = "listening"
			changed = true
		}
	case TypeLLM:
		if e.Text != "" && current.Reply == "" || e.Text != "" && isEmotion(current.Reply) {
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
		} else if e.State == TTSSentenceStart && e.Text != "" && !strings.HasPrefix(e.Text, "%") {
			// (A sentence starting with % is the server narrating a tool call, not something said.)
			// The words the server is about to say. The llm messages carry only an emotion, so this
			// is the only place the reply's text arrives; a sentence at a time, joined as they come.
			if current.Reply == "" || isEmotion(current.Reply) {
				current.Reply = e.Text
			} else {
				current.Reply += " " + e.Text
			}
			current.Phase = "replying"
			changed = true
		} else if e.State == TTSStop {
			if config.Get().Microphone.PipelineEnds {
				current.Phase = "listening"
			} else {
				current.Phase = "idle"
			}
			changed = true
		}
	}

	if changed {
		slog.Info("xiaozhi screen: fire", "phase", current.Phase, "heard", current.Heard, "reply", current.Reply)
		fire()
	}
}

// reset clears the phase when a turn ends, keeping Heard and Reply so the
// display can let them linger through the idle that follows.
func resetScreen() {
	slog.Info("xiaozhi screen: reset to idle")
	current.Phase = "idle"
	fire()
}

// isEmotion reports whether s is only the emoji an llm message carries in place of words.
func isEmotion(s string) bool {
	for _, r := range s {
		if r < 0x2000 {
			return false
		}
	}
	return s != ""
}
