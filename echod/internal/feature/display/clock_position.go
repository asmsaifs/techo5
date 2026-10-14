//go:build !dot

package display

import (
	"log/slog"

	esphome "github.com/ygelfand/go-esphome-device"

	"github.com/HuskerMinion/techo5/echod/internal/config"
)

// Clock position, where the home screen's clock sits: out of the way of a photo behind it. Each
// device has its own choices (clockPositions): the Shows' bottom corners, the Spot's foot.

// clockPosition is one choice, and what the config keeps for it; the center is kept as nothing.
type clockPosition struct {
	label, value string
}

func clockPositionIndex() int {
	v := config.Get().Screen.ClockPosition
	for i, p := range clockPositions {
		if p.value == v {
			return i
		}
	}
	return 0
}

func clockPositionOptions() []string {
	out := make([]string, len(clockPositions))
	for i, p := range clockPositions {
		out[i] = p.label
	}
	return out
}

func setClockPosition(s *esphome.Select, i int) {
	if i < 0 || i >= len(clockPositions) {
		return
	}
	if err := config.Set().Screen().ClockPosition(clockPositions[i].value); err != nil {
		slog.Error("saving the clock position failed", "err", err)
		return
	}
	if s != nil {
		s.Set(clockPositions[i].label)
	}
}

// clockPositionSelect is Clock position in Home Assistant.
func clockPositionSelect(wake func()) *esphome.Select {
	position := &esphome.Select{
		Base: esphome.Base{
			ObjectID: "screen_clock_position",
			Name:     "Clock position",
			Icon:     "mdi:clock-outline",
			Category: esphome.CategoryConfig,
		},
		Options: clockPositionOptions(),
	}
	position.OnCommand = func(v string) {
		for i, p := range clockPositions {
			if p.label == v {
				setClockPosition(position, i)
				wake()
				return
			}
		}
	}
	return position
}
