//go:build !dot

package display

import (
	"log/slog"

	esphome "github.com/ygelfand/go-esphome-device"

	"github.com/HuskerMinion/techo5/echod/internal/config"
)

// What a voice turn looks like on the screen: Classic, the title and the words, or Equalizer, bars
// moving with the voice being heard and the answer being spoken. Classic until somebody chooses.

// turnStyles are the choices, with what the config keeps for each; Classic is kept as nothing.
var turnStyles = []struct {
	label string
	value string
}{
	{"Classic", ""},
	{"Wave", "wave"},
	{"Bars", "equalizer"},
}

func turnStyleOptions() []string {
	out := make([]string, len(turnStyles))
	for i, t := range turnStyles {
		out[i] = t.label
	}
	return out
}

// turnStyleIndex is the saved choice's place in turnStyles; a value no choice has reads as Classic.
func turnStyleIndex() int {
	v := config.Get().Screen.TurnStyle
	for i, t := range turnStyles {
		if t.value == v {
			return i
		}
	}
	return 0
}

// setTurnStyle saves the choice at i and shows it in Home Assistant.
func setTurnStyle(s *esphome.Select, i int) {
	if i < 0 || i >= len(turnStyles) {
		return
	}
	if err := config.Set().Screen().TurnStyle(turnStyles[i].value); err != nil {
		slog.Error("saving the turn screen failed", "err", err)
		return
	}
	if s != nil {
		s.Set(turnStyles[i].label)
	}
}
