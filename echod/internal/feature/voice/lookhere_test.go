package voice

import (
	"path/filepath"
	"testing"

	"github.com/HuskerMinion/techo5/echod/internal/config"
)

// Going home, a camera, and - where the screen opens it on the words - the forecast leave the answer
// on the screen rather than listening again; anything else may still be followed up.
func TestPageAskedSkipsListeningAgain(t *testing.T) {
	config.Use(filepath.Join(t.TempDir(), "state.json"))
	for heard, want := range map[string]bool{
		"go home":                     true,
		"show the weather":            true,
		"show the radar":              true,
		"show the front door camera":  true,
		"set a timer for ten minutes": false,
	} {
		if got := pageAsked(heard); got != want {
			t.Errorf("with Home Assistant, %q: %v, want %v", heard, got, want)
		}
	}

	if err := config.Set().Brain().Set(config.Brain{Mode: config.BrainDirect, STT: "x:1", TTS: "x:1", LLM: "http://x"}); err != nil {
		t.Fatal(err)
	}
	if !pageAsked("go home") {
		t.Error("going home is followed up in direct mode")
	}
	// Direct mode leaves the weather to the assistant, which says itself when it shows a page.
	if pageAsked("what's the weather tomorrow") {
		t.Error("a weather question is kept from a follow-up in direct mode")
	}
}
