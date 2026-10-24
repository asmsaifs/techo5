package xiaozhi

import (
	"log/slog"
	"strings"
	"sync"

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
// Publication is synchronous; listeners must not block or re-enter screen updates.
var Changed hook.Hook[State]

// screenMu serializes the microphone and socket goroutines, including publication
// order. Listeners receive State by value and do not access current.
var screenMu sync.Mutex
var current State

// fire emits the current state if anything changed.
func fire() {
	Changed.Emit(current)
}

// onScreenEvent updates the screen state from a server event.
// Called from onEvent on the socket's goroutine.
func onScreenEvent(e Event) {
	screenMu.Lock()
	defer screenMu.Unlock()
	changed := false
	switch e.Type {
	case TypeSTT:
		if e.Text != "" && e.Text != current.Heard {
			slog.Info("xiaozhi screen: stt", "text", e.Text)
			current.Heard = e.Text
			current.Reply = "" // a new request; the last answer is not this one's
			if current.Phase != "replying" {
				current.Phase = "thinking"
			}
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
			if current.Phase != "replying" {
				current.Phase = "thinking"
			}
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
			// A TTS event does not open a microphone. FollowUp publishes
			// listening only when its own subscription is ready.
			current.Phase = "idle"
			changed = true
		}
	}

	if changed {
		slog.Info("xiaozhi screen: fire", "phase", current.Phase, "heard", current.Heard, "reply", current.Reply)
		fire()
	}
}

// startScreen is called when captured audio can reach the uplink, not after STT.
func startScreen() {
	screenMu.Lock()
	defer screenMu.Unlock()
	current = State{Phase: "listening"}
	fire()
}

// finishScreen leaves a response already arriving alone. A natural endpoint
// means waiting for recognition; cancellation or a failed capture means idle.
func finishScreen(waiting bool) {
	screenMu.Lock()
	defer screenMu.Unlock()
	if waiting && current.Phase != "listening" {
		return
	}
	current.Phase = "idle"
	if waiting {
		current.Phase = "thinking"
	}
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
