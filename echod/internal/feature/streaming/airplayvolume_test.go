package streaming

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/HuskerMinion/techo5/echod/internal/config"
)

// The AirPlay slider, -30 to 0 dB, is spread evenly over the device's 30 steps; mute and anything under
// the bottom is step 0, and data that is not a number is passed over.
func TestAirPlayStep(t *testing.T) {
	for _, c := range []struct {
		data string
		step int
		ok   bool
	}{
		{"0.00,0.00,-30.00,0.00", 30, true},
		{"-30.00,-30.00,-30.00,0.00", 0, true},
		{"-15.00,-15.00,-30.00,0.00", 15, true},
		{"-7.50,-7.50,-30.00,0.00", 23, true}, // 22.5 rounds up
		{"-0.40,0,0,0", 30, true},
		{"-144.00,-144.00,-30.00,0.00", 0, true},
		{"-31.5,0,0,0", 0, true},
		{"3.0,0,0,0", 30, true},
		{" -20 ", 10, true},
		{"", 0, false},
		{"loud,1,2,3", 0, false},
		{"NaN,0,0,0", 0, false},
		{"-Inf,0,0,0", 0, false},
	} {
		if step, ok := airplayStep(c.data); step != c.step || ok != c.ok {
			t.Errorf("airplayStep(%q) = %d, %v; want %d, %v", c.data, step, ok, c.step, c.ok)
		}
	}
}

// A pvol item reaches the volume, only with AirPlay sets the volume on, and no other item does.
func TestAirPlayVolumeItem(t *testing.T) {
	config.Use(filepath.Join(t.TempDir(), "state.json"))
	var set []int
	was := setAirPlayVolume
	t.Cleanup(func() { setAirPlayVolume = was })
	setAirPlayVolume = func(to int) { set = append(set, to) }

	stream := item("ssnc", "pvol", "-20.00,-20.00,-30.00,0.00") + item("core", "minm", "Take It Easy") +
		item("ssnc", "pvol", "-144.00,-144.00,-30.00,0.00") + item("ssnc", "pvol", "junk") +
		item("ssnc", "pvol", "0.00,0.00,-30.00,0.00")
	run := func() {
		set = nil
		session := &airplaySession{volume: airplayVolume}
		followAirPlayMetadata(strings.NewReader(stream), func(string, string, string) {}, func([]byte) {}, session)
	}

	if config.Get().Streaming.AirPlaySetsVolume {
		t.Fatal("a new device has AirPlay set the volume")
	}
	run()
	if len(set) != 0 {
		t.Errorf("off, the slider set the volume to %v", set)
	}

	if err := config.Set().Streaming().AirPlaySetsVolume(true); err != nil {
		t.Fatal(err)
	}
	run()
	if len(set) != 3 || set[0] != 10 || set[1] != 0 || set[2] != 30 {
		t.Errorf("on, the slider set the volume to %v", set)
	}
}

// The configuration has shairport-sync pass the audio as it comes only with the setting on, in both
// receivers, and keeps the metadata pipe that tells of the slider.
func TestShairportConfIgnoresVolume(t *testing.T) {
	for _, airplay2 := range []bool{false, true} {
		off := shairportConf("Kitchen", "/run/x", airplay2, false)
		on := shairportConf("Kitchen", "/run/x", airplay2, true)
		if strings.Contains(off, "ignore_volume_control") {
			t.Errorf("off (AirPlay 2 %v), the volume is ignored:\n%s", airplay2, off)
		}
		if !strings.Contains(on, `ignore_volume_control = "yes";`) {
			t.Errorf("on (AirPlay 2 %v), the volume is not ignored:\n%s", airplay2, on)
		}
		// It belongs to the general section, ahead of the next one.
		if i := strings.Index(on, "ignore_volume_control"); i < 0 || i > strings.Index(on, "sessioncontrol") {
			t.Errorf("on (AirPlay 2 %v), the setting is outside the general section:\n%s", airplay2, on)
		}
		if !strings.Contains(on, "metadata = {") || !strings.Contains(on, `enabled = "yes";`) {
			t.Errorf("on (AirPlay 2 %v), no metadata:\n%s", airplay2, on)
		}
	}
}

// The switch is the setting's, off as it comes, and turning it on saves it and wakes the receivers.
func TestTheAirPlaySetsVolumeSwitch(t *testing.T) {
	config.Use(filepath.Join(t.TempDir(), "state.json"))
	f := build()
	f.Restore(config.Get())
	if f.airplayVolume.ObjectID != "airplay_sets_volume" || f.airplayVolume.Get() {
		t.Errorf("switch %q on %v from the start", f.airplayVolume.ObjectID, f.airplayVolume.Get())
	}
	f.SetAirPlaySetsVolume(true)
	if !config.Get().Streaming.AirPlaySetsVolume || !f.airplayVolume.Get() {
		t.Error("turned on, not kept")
	}
	select {
	case <-f.wake:
	default:
		t.Error("the receivers were not told to start again")
	}
	f.SetAirPlaySetsVolume(false)
	if config.Get().Streaming.AirPlaySetsVolume {
		t.Error("turned off, still kept")
	}
}
