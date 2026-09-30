package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// A file written by a newer build keeps what this build does not know when this build saves, at the
// top and inside objects it does know; and what this build clears stays cleared.
func TestSettingsANewerBuildWroteAreKept(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	newer := `{
	  "brain": {"mode": "direct", "stt": "host:1", "key": "secret"},
	  "home": {"weather": "weather.x", "garden_sensor": "sensor.soil"},
	  "from_the_future": {"level": 3},
	  "speaker": {"volume": 5}
	}`
	if err := os.WriteFile(path, []byte(newer), 0o644); err != nil {
		t.Fatal(err)
	}
	st, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	// A change of this build's own, and a setting it clears.
	if err := st.Set().Speaker().Volume(7); err != nil {
		t.Fatal(err)
	}
	if err := st.Set().Brain().SetKey(""); err != nil {
		t.Fatal(err)
	}

	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var got map[string]any
	if err := json.Unmarshal(b, &got); err != nil {
		t.Fatal(err)
	}
	if f, ok := got["from_the_future"].(map[string]any); !ok || f["level"] != 3.0 {
		t.Errorf("a setting this build has no field for was dropped: %v", got["from_the_future"])
	}
	home, _ := got["home"].(map[string]any)
	if home["garden_sensor"] != "sensor.soil" || home["weather"] != "weather.x" {
		t.Errorf("home = %v", home)
	}
	brain, _ := got["brain"].(map[string]any)
	if _, kept := brain["key"]; kept {
		t.Error("a key this build cleared came back from the old file")
	}
	if brain["stt"] != "host:1" {
		t.Errorf("brain = %v", brain)
	}
	if sp, _ := got["speaker"].(map[string]any); sp["volume"] != 7.0 {
		t.Errorf("this build's own change was lost: %v", got["speaker"])
	}
}
