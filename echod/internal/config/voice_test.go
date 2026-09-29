package config

import "testing"

// The switch is the only thing standing between a new device and a room's audio going to a third
// party, so its default has to be the backend that was already there. Getting this wrong is silent
// and permanent: the device works, and it is working somewhere else.
func TestVoiceDefaultsToHomeAssistant(t *testing.T) {
	if got := Defaults().Voice.Backend; got != DefaultVoiceBackend {
		t.Errorf("backend = %q, want %q", got, DefaultVoiceBackend)
	}
	if DefaultVoiceBackend != BackendHomeAssistant {
		t.Errorf("the default is %q: every device would be switched to a backend nobody asked for",
			DefaultVoiceBackend)
	}
}

// A file with no voice section at all is every device that existed before this setting, and it has
// to come up exactly as it did: on Home Assistant, with the xiaozhi client still off.
func TestAVoiceLessConfigKeepsTheDefaults(t *testing.T) {
	if got := (VoiceBackend("")).Settle(); got != DefaultVoiceBackend {
		t.Errorf("settled %q, want %q", got, DefaultVoiceBackend)
	}
}

// A backend written by a build that had a third one has to settle rather than reach the select: an
// option the entity cannot offer is a row in Home Assistant that cannot be changed at all.
func TestAnUnknownBackendSettles(t *testing.T) {
	for _, tc := range []struct {
		name string
		in   VoiceBackend
		want VoiceBackend
	}{
		{"empty", "", BackendHomeAssistant},
		{"home assistant", BackendHomeAssistant, BackendHomeAssistant},
		{"xiaozhi", BackendXiaozhi, BackendXiaozhi},
		{"from a newer build", "somebody_elses_backend", BackendHomeAssistant},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.in.Settle(); got != tc.want {
				t.Errorf("%q settled to %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

// A select speaks labels and everything else speaks values, so the two have to meet for the backend
// as they do for every other setting. This is the path the entity and the sheet both take.
func TestVoiceBackendLabelsRoundTrip(t *testing.T) {
	for _, b := range VoiceBackends() {
		got, ok := ByLabel(VoiceBackends(), b.Label())
		if !ok {
			t.Errorf("%q is not resolvable from the list on offer", b.Label())
			continue
		}
		if got != b {
			t.Errorf("label %q resolved to %q, want %q", b.Label(), got, b)
		}
	}
	if labels := Labels(VoiceBackends()); len(labels) != 2 {
		t.Errorf("the select offers %v, want the two backends", labels)
	}
}

// Choosing a backend has to survive a restart, or the device comes back up talking to the other one
// with no switch having been moved.
func TestTheBackendSurvivesAReload(t *testing.T) {
	path := t.TempDir() + "/state.json"
	st, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if err := st.Set().Voice().Backend(BackendXiaozhi); err != nil {
		t.Fatalf("saving the backend: %v", err)
	}

	again, err := Load(path)
	if err != nil {
		t.Fatalf("reloading: %v", err)
	}
	if got := again.Get().Voice.Backend; got != BackendXiaozhi {
		t.Errorf("reloaded backend = %q, want %q", got, BackendXiaozhi)
	}
}

// An unrecognised value in the file is not written back as-is: the writer settles what it is given,
// so a value from a newer build cannot be preserved by accident and then acted on.
func TestSavingAnUnknownBackendSettlesIt(t *testing.T) {
	path := t.TempDir() + "/state.json"
	st, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if err := st.Set().Voice().Backend("from_a_newer_build"); err != nil {
		t.Fatalf("saving the backend: %v", err)
	}
	if got := st.Get().Voice.Backend; got != DefaultVoiceBackend {
		t.Errorf("backend = %q, want it settled to %q", got, DefaultVoiceBackend)
	}
}
