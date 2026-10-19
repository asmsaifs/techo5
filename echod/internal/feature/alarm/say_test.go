package alarm

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/HuskerMinion/techo5/echod/internal/config"
)

func TestWhatAnAlarmSays(t *testing.T) {
	at := time.Date(2026, 10, 17, 7, 5, 0, 0, time.Local)
	for _, c := range []struct {
		label   string
		say     config.AlarmSay
		clock24 bool
		en      bool
		want    string
	}{
		{"Take the trash out", config.SayWords, false, true, "Take the trash out"},
		{"", config.SayWords, false, true, ""},
		{"Alarm", config.SayWords, false, true, ""},
		{"  Wake up alarm ", config.SayWords, false, true, ""},
		{"Take the trash out", config.SayTime, false, true, "7:05 AM. Take the trash out"},
		{"Take the trash out", config.SayTime, true, true, "07:05. Take the trash out"},
		{"", config.SayTime, false, true, "7:05 AM"},
		{"Alarm", config.SayTime, true, true, "07:05"},
		{"Take the trash out", config.SayNothing, false, true, ""},
		{"", config.SayNothing, true, true, ""},
		{"Take the trash out", config.SayTime, false, false, "7:05. Take the trash out"},
		{"", config.SayTime, true, false, "07:05"},
	} {
		if got := spoken(c.label, at, c.say, c.clock24, c.en); got != c.want {
			t.Errorf("%q, %q, 24h %v: says %q, want %q", c.label, c.say, c.clock24, got, c.want)
		}
	}
}

// A device that never chose says its alarms' words, as alarms always have.
func TestTheWordsAreTheDefault(t *testing.T) {
	if got := config.Defaults().Alarms.Say; got != config.SayWords {
		t.Errorf("a new device's alarms say %q, want their words", got)
	}
}

func TestSetSayKeepsTheChoice(t *testing.T) {
	config.Use(filepath.Join(t.TempDir(), "state.json"))
	a := Get()
	a.SetSay(config.SayTime)
	if got := a.Say(); got != config.SayTime {
		t.Errorf("after choosing the time: %q", got)
	}
	if got := a.words.Get(); got != config.SayTime.Label() {
		t.Errorf("Home Assistant shows %q, want %q", got, config.SayTime.Label())
	}
	a.SetSay("shout")
	if got := a.Say(); got != config.SayTime {
		t.Errorf("an unknown choice changed it to %q", got)
	}
	a.SetSay(config.SayWords)
}
