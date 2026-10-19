package config

import (
	"encoding/json"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
)

func TestRealtimeTravelsWithoutItsSecretsOrItsSlots(t *testing.T) {
	here := Defaults()
	here.Realtime = Realtime{Provider: "openai", Key: "sk-mine", MCPToken: "ha-token", UsedDay: "2026-10-18", UsedSeconds: 600}
	here.Wake.Words = []WakeWord{DefaultWakeWord()}

	from := Defaults()
	from.Realtime = Realtime{Provider: "openai", Key: "sk-theirs", MCPToken: "their-token", DailyMinutes: 45}
	from.Wake.Words = []WakeWord{DefaultWakeWord()}
	from.Wake.Words[0].Realtime = true
	file, err := Export(from, "", "v0")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(file), "sk-theirs") || strings.Contains(string(file), "their-token") {
		t.Fatal("a key or a token left the device in the file")
	}
	r, err := Restored(here, file, "")
	if err != nil {
		t.Fatal(err)
	}
	got := r.Config.Realtime
	if got.Key != "sk-mine" || got.MCPToken != "ha-token" || got.DailyMinutes != 45 || got.UsedSeconds != 600 {
		t.Errorf("%+v", got)
	}
	if r.Config.Wake.Words[0].Realtime {
		t.Error("a file sent a slot's voice to a company")
	}

	// Another provider in the file: this device's key is not sent there.
	from.Realtime.Provider = "gemini"
	file, _ = Export(from, "", "v0")
	r, _ = Restored(here, file, "")
	if r.Config.Realtime.Key != "" || !strings.Contains(strings.Join(r.Reenter, " "), "realtime") {
		t.Errorf("key %q, reenter %v", r.Config.Realtime.Key, r.Reenter)
	}
	if len(r.Servers) == 0 {
		t.Error("the provider's change was not shown")
	}
}

func TestRealtimeWriter(t *testing.T) {
	st, err := Load(filepath.Join(t.TempDir(), "state.json"))
	if err != nil {
		t.Fatal(err)
	}
	w := st.Set().Realtime()
	_ = w.Set("openai", "", "", "", 20)
	_ = w.SetKey("sk-1")
	_ = w.Set("openai", "", "gpt-realtime", "marin", 9999)
	if r := st.Get().Realtime; r.Key != "sk-1" || r.Daily() != MaxDailyMinutes || !r.Ready() {
		t.Fatalf("%+v", r)
	}
	_ = w.Set("xai", "", "", "", 0)
	if r := st.Get().Realtime; r.Key != "" || r.Ready() || r.Daily() != DefaultDailyMinutes {
		t.Fatalf("another provider kept the key: %+v", r)
	}

	day := time.Date(2026, 10, 18, 9, 0, 0, 0, time.Local)
	_ = w.Used(90, day)
	_ = w.Used(30, day.Add(time.Hour))
	if left := st.Get().Realtime.SecondsLeft(day); left != DefaultDailyMinutes*60-120 {
		t.Fatalf("%d left", left)
	}
	_ = w.Used(10, day.Add(24*time.Hour))
	if r := st.Get().Realtime; r.UsedSeconds != 10 || r.SecondsLeft(day.Add(24*time.Hour)) != DefaultDailyMinutes*60-10 {
		t.Fatalf("a new day did not start over: %+v", r)
	}

	// A slot with no wake word is not made by choosing it.
	n := len(st.Get().Wake.Words)
	if err := st.Set().Wake(n + 1).Realtime(true); err == nil || len(st.Get().Wake.Words) != n {
		t.Fatalf("an empty slot was made: %v, %d words", err, len(st.Get().Wake.Words))
	}
	if err := st.Set().Wake(1).Threshold(0.5); err != nil { // a second slot, with no word yet
		t.Fatal(err)
	}
	if err := st.Set().Wake(1).Realtime(true); err == nil {
		t.Fatal("a slot with no word was chosen")
	}
	if err := st.Set().Wake(1).ID("hey_jarvis"); err != nil {
		t.Fatal(err)
	}
	if err := st.Set().Wake(1).Realtime(true); err != nil {
		t.Fatal(err)
	}
	if ws := st.Get().Wake.Words; len(ws) != 2 || !ws[1].Realtime || ws[0].Realtime {
		t.Fatalf("%+v", ws)
	}
}

// A file that moves realtime voice to another provider or relay leaves every slot off it; one that keeps
// them leaves the device's choice as it was.
func TestARestoreThatMovesRealtimeChoosesNoSlot(t *testing.T) {
	here := Defaults()
	here.Realtime = Realtime{Provider: "openai", Key: "sk-mine"}
	here.Wake.Words = []WakeWord{DefaultWakeWord()}
	here.Wake.Words[0].Realtime = true

	from := Defaults()
	from.Realtime = Realtime{Provider: "openai"}
	from.Wake.Words = []WakeWord{DefaultWakeWord()}
	file, _ := Export(from, "", "v0")
	r, _ := Restored(here, file, "")
	if !r.Config.Wake.Words[0].Realtime {
		t.Fatal("the same provider: the device's choice was lost")
	}
	from.Realtime.Endpoint = "wss://elsewhere.example/rt"
	file, _ = Export(from, "", "v0")
	r, _ = Restored(here, file, "")
	if r.Config.Wake.Words[0].Realtime {
		t.Fatal("a file sent the slot's voice to its own relay")
	}
}

// A file from before realtime voice leaves the device's realtime voice as it was: provider, key and slots.
func TestARestoreFromBeforeRealtimeKeepsIt(t *testing.T) {
	here := Defaults()
	here.Realtime = Realtime{Provider: "gemini", Key: "g-mine", Endpoint: "wss://relay.example/rt"}
	here.Wake.Words = []WakeWord{DefaultWakeWord()}
	here.Wake.Words[0].Realtime = true

	from := Defaults()
	from.Wake.Words = []WakeWord{DefaultWakeWord()}
	file, _ := Export(from, "", "v0")
	var f map[string]json.RawMessage
	_ = json.Unmarshal(file, &f)
	var settings map[string]json.RawMessage
	_ = json.Unmarshal(f["settings"], &settings)
	delete(settings, "realtime")
	f["settings"], _ = json.Marshal(settings)
	old, _ := json.Marshal(f)

	r, err := Restored(here, old, "")
	if err != nil {
		t.Fatal(err)
	}
	if rt := r.Config.Realtime; rt.Provider != "gemini" || rt.Key != "g-mine" || rt.Endpoint != here.Realtime.Endpoint {
		t.Fatalf("%+v", rt)
	}
	if !r.Config.Wake.Words[0].Realtime || slices.Contains(r.Reenter, "realtime voice's key") {
		t.Fatalf("slot %v, re-enter %v", r.Config.Wake.Words[0].Realtime, r.Reenter)
	}
}
