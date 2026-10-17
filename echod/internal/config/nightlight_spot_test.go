//go:build spot

package config

import (
	"fmt"
	"math"
	"os"
	"path/filepath"
	"testing"
)

// The table is the nearest step by ratio of backlight, worked out here from the two scales.
func TestTheNightLightRescaleIsTheNearestStep(t *testing.T) {
	old := []float64{6, 10, 14, 20, 28, 38, 50, 64, 80, 100}
	steps := []float64{3, 4, 6, 8, 12, 16, 22, 30, 40, 50}
	for i, b := range old {
		best := 0
		for j, s := range steps {
			if math.Abs(math.Log(s/b)) < math.Abs(math.Log(steps[best]/b)) {
				best = j
			}
		}
		if got := nightLightRescale[i]; got != best+1 {
			t.Errorf("old step %d (backlight %v) goes to step %d, want %d", i+1, b, got, best+1)
		}
	}
}

// A Spot's night clock level saved on the old scale is moved to the new one once; none set stays
// none, a file already moved is left alone, and a level set after the move is never moved again.
func TestASavedNightLightLevelIsRescaledOnce(t *testing.T) {
	load := func(t *testing.T, path string) Config {
		t.Helper()
		st, err := Load(path)
		if err != nil {
			t.Fatal(err)
		}
		return st.Get()
	}
	for _, tc := range []struct{ saved, want int }{{0, 0}, {1, 3}, {2, 5}, {4, 7}, {5, 8}, {7, 10}, {10, 10}} {
		p := filepath.Join(t.TempDir(), "state.json")
		if err := os.WriteFile(p, []byte(fmt.Sprintf(`{"screen":{"night_light_level":%d}}`, tc.saved)), 0o600); err != nil {
			t.Fatal(err)
		}
		if c := load(t, p).Screen; c.NightLightLevel != tc.want || !c.NightLightRescaled {
			t.Errorf("saved %d: level %d rescaled %v, want %d", tc.saved, c.NightLightLevel, c.NightLightRescaled, tc.want)
		}
	}

	p := filepath.Join(t.TempDir(), "state.json")
	if err := os.WriteFile(p, []byte(`{"screen":{"night_light_level":5,"night_light_rescaled":true}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if got := load(t, p).Screen.NightLightLevel; got != 5 {
		t.Errorf("a level already on the new scale was moved to %d", got)
	}

	// Moved at load, then saved with the next change: the next start finds it moved.
	p = filepath.Join(t.TempDir(), "state.json")
	if err := os.WriteFile(p, []byte(`{"screen":{"night_light_level":5}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	st, err := Load(p)
	if err != nil {
		t.Fatal(err)
	}
	if err := st.Set().Screen().NightLightLevel(2); err != nil {
		t.Fatal(err)
	}
	if c := load(t, p).Screen; c.NightLightLevel != 2 || !c.NightLightRescaled {
		t.Errorf("after a change and a restart: level %d rescaled %v, want 2 and true", c.NightLightLevel, c.NightLightRescaled)
	}

	// A new device has nothing to move and starts on the new scale.
	if c := load(t, filepath.Join(t.TempDir(), "none.json")).Screen; c.NightLightLevel != 0 || !c.NightLightRescaled {
		t.Errorf("a new device: level %d rescaled %v", c.NightLightLevel, c.NightLightRescaled)
	}
}
