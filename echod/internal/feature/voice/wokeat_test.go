package voice

import (
	"path/filepath"
	"testing"
	"time"

	esphome "github.com/ygelfand/go-esphome-device"

	"github.com/HuskerMinion/techo5/echod/internal/config"
)

// WokeAt carries how long it waited, so the turn sends that much more of the history.
func TestWokeAtCarriesTheWait(t *testing.T) {
	c := newConversation(&esphome.VoiceSatellite{})
	at := time.Now().Add(-300 * time.Millisecond)
	c.WokeAt(0, at)
	e := <-c.events
	if e.kind != evStart || !e.woke || !e.at.Equal(at) {
		t.Fatalf("posted %+v", e)
	}
	if e.waited < 300*time.Millisecond || e.waited > time.Second {
		t.Errorf("waited %v, want about 300ms", e.waited)
	}
}

// A device with no assistant to take a turn cannot answer, so it must not claim a wake word.
func TestCanAnswerNeedsAnAssistant(t *testing.T) {
	config.Use(filepath.Join(t.TempDir(), "state.json"))
	vs := &esphome.VoiceSatellite{ActiveWakeWords: []string{"okay_nabu"}}
	v := &Voice{vs: vs, turn: newConversation(vs)}
	if v.CanAnswer(0) {
		t.Error("with Home Assistant not listening, the device says it can answer")
	}
}

// A slot chosen for realtime voice can be answered when realtime voice is ready, Home Assistant or not,
// and not when it is not.
func TestCanAnswerARealtimeSlot(t *testing.T) {
	config.Use(filepath.Join(t.TempDir(), "state.json"))
	vs := &esphome.VoiceSatellite{ActiveWakeWords: []string{"okay_nabu"}}
	v := &Voice{vs: vs, turn: newConversation(vs)}
	_ = config.Set().Wake(0).Realtime(true)
	if v.CanAnswer(0) {
		t.Error("realtime voice not set up, and the device says it can answer")
	}
	_ = config.Set().Realtime().Set("openai", "", "", "", 0)
	_ = config.Set().Realtime().SetKey("sk-test")
	if !v.CanAnswer(0) {
		t.Error("realtime voice ready, and the device says it cannot answer without Home Assistant")
	}
}
