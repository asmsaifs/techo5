package voice

import (
	"path/filepath"
	"testing"

	esphome "github.com/ygelfand/go-esphome-device"

	"github.com/HuskerMinion/techo5/echod/internal/config"
	"github.com/HuskerMinion/techo5/echod/internal/feature/wakeword"
	"github.com/HuskerMinion/techo5/echod/internal/lib/wake"
)

// What a device advertises as active when nobody has touched the wake word decides whether a fresh
// install can be spoken to at all, and it must not come down to which model sorts first. A new device
// is configured for config.DefaultWakeID; without that model it falls back to the shipped model, and
// without either to whatever is installed.
func TestWakeWordsPreselectsTheDefault(t *testing.T) {
	for name, tc := range map[string]struct {
		installed []string
		want      string
	}{
		"the configured word is installed": {[]string{"hey_jarvis", config.DefaultWakeID, wake.DefaultModel}, config.DefaultWakeID},
		"the configured word sorts last":   {[]string{wake.DefaultModel, "hey_jarvis", config.DefaultWakeID}, config.DefaultWakeID},
		"only the shipped model":           {[]string{"hey_jarvis", wake.DefaultModel}, wake.DefaultModel},
		"neither is installed":             {[]string{"hey_jarvis"}, "hey_jarvis"},
		"nothing installed":                {nil, ""},
	} {
		models := make([]wake.Model, 0, len(tc.installed))
		for _, id := range tc.installed {
			models = append(models, wake.Model{ID: id, Phrase: id})
		}

		active := activeWakeWords(models, wakeword.Slots)

		switch {
		case tc.want == "":
			if len(active) != 0 {
				t.Errorf("%s: listening for %v with nothing installed", name, active)
			}
		case len(active) != 1 || active[0] != tc.want:
			t.Errorf("%s: listening for %v, want just %q", name, active, tc.want)
		}
	}
}

// "No wake word" chosen on purpose survives a restart: the default a fresh device starts with must not
// come back over it. Choosing a word again clears it.
func TestNoWakeWordStaysChosen(t *testing.T) {
	config.Use(filepath.Join(t.TempDir(), "state.json"))
	models := []wake.Model{{ID: config.DefaultWakeID, Phrase: "default"}, {ID: "hey_jarvis", Phrase: "jarvis"}}

	v := &Voice{vs: &esphome.VoiceSatellite{}}
	v.OnWakeWord(func(ids []string) []string { return ids }, func() {})

	v.vs.OnSetActiveWakeWords(nil)
	if got := activeWakeWords(models, wakeword.Slots); len(got) != 0 {
		t.Fatalf("after No wake word, a restart listens for %v", got)
	}

	v.vs.OnSetActiveWakeWords([]string{"hey_jarvis"})
	if got := activeWakeWords(models, wakeword.Slots); len(got) != 1 || got[0] != "hey_jarvis" {
		t.Fatalf("after choosing a word: %v", got)
	}
	if config.Get().Wake.NoneChosen {
		t.Error("choosing a word left No wake word recorded")
	}
}
