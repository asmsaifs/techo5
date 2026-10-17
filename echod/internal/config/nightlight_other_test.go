//go:build !spot

package config

import (
	"os"
	"path/filepath"
	"testing"
)

// Only the Spot's night clock changed scale: a Show's night light level is left as it was saved.
func TestANightLightLevelOffTheSpotIsLeftAlone(t *testing.T) {
	p := filepath.Join(t.TempDir(), "state.json")
	if err := os.WriteFile(p, []byte(`{"screen":{"night_light_level":5}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	st, err := Load(p)
	if err != nil {
		t.Fatal(err)
	}
	if c := st.Get().Screen; c.NightLightLevel != 5 || c.NightLightRescaled {
		t.Errorf("level %d rescaled %v, want 5 and untouched", c.NightLightLevel, c.NightLightRescaled)
	}
}
