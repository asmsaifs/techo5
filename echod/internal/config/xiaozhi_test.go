package config

import "testing"

// A device nobody has heard of xiaozhi comes up with the client off and the codec at the settings
// M0 was verified at, so a device that has never been configured is one that would work.
func TestXiaozhiDefaults(t *testing.T) {
	got := Defaults().Xiaozhi

	if got.Enabled != DefaultXiaozhiEnabled {
		t.Errorf("enabled = %v, want %v: it sends microphone audio to a third party and must be asked to",
			got.Enabled, DefaultXiaozhiEnabled)
	}
	if got.Bitrate != DefaultXiaozhiBitrate || got.Complexity != DefaultXiaozhiComplexity || got.FrameMS != DefaultXiaozhiFrameMS {
		t.Errorf("codec = %d bps / complexity %d / %d ms, want the measured %d / %d / %d",
			got.Bitrate, got.Complexity, got.FrameMS,
			DefaultXiaozhiBitrate, DefaultXiaozhiComplexity, DefaultXiaozhiFrameMS)
	}
	if got.Host != "" {
		t.Errorf("host = %q, want empty so the official cloud is the default", got.Host)
	}
}

// A frame length of zero, or a file that never had one, still has to produce a frame. A hello that
// says frame_duration 0 is one the server has to guess about, and it will guess worse than the
// measured value this device already knows.
func TestFrameFallsBackToTheMeasuredValue(t *testing.T) {
	for _, tc := range []struct {
		name string
		ms   int
		want int
	}{
		{"unset", 0, DefaultXiaozhiFrameMS},
		{"negative", -60, DefaultXiaozhiFrameMS},
		{"set", 20, 20},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := (Xiaozhi{FrameMS: tc.ms}).Frame(); got != tc.want {
				t.Errorf("Frame() = %d, want %d", got, tc.want)
			}
		})
	}
}

// Everything the client has to remember between boots is in this one struct, so it is worth a round
// trip: a client id that did not survive a restart is a device that has to be activated again.
func TestXiaozhiSettingsSurviveAReload(t *testing.T) {
	path := t.TempDir() + "/state.json"
	st, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	for _, set := range []func() error{
		func() error { return st.Set().Xiaozhi().Enabled(true) },
		func() error { return st.Set().Xiaozhi().Host("xiaozhi.example") },
		func() error { return st.Set().Xiaozhi().Token("a-token") },
		func() error { return st.Set().Xiaozhi().ClientID("d5a1e0f4-1b1c-4b0e-9a1e-6a1b2c3d4e5f") },
		func() error { return st.Set().Xiaozhi().Activated(true) },
	} {
		if err := set(); err != nil {
			t.Fatalf("saving a xiaozhi setting: %v", err)
		}
	}

	again, err := Load(path)
	if err != nil {
		t.Fatalf("reloading: %v", err)
	}
	got := again.Get().Xiaozhi

	want := Xiaozhi{
		Enabled: true, Host: "xiaozhi.example", Token: "a-token",
		ClientID: "d5a1e0f4-1b1c-4b0e-9a1e-6a1b2c3d4e5f", Activated: true,
		Bitrate: DefaultXiaozhiBitrate, Complexity: DefaultXiaozhiComplexity, FrameMS: DefaultXiaozhiFrameMS,
	}
	if got != want {
		t.Errorf("reloaded %+v, want %+v", got, want)
	}
}
