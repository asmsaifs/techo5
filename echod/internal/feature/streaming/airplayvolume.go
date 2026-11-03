package streaming

import (
	"log/slog"
	"math"
	"strconv"
	"strings"

	"github.com/HuskerMinion/techo5/echod/internal/config"
	"github.com/HuskerMinion/techo5/echod/internal/feature/media"
)

// AirPlay sets the volume (#312). An iPhone's AirPlay slider only scales the stream inside
// shairport-sync, and the device's own volume, the one Home Assistant shows, stays where it is, so a
// quiet stream has two volumes to turn up. With the setting on, shairport-sync passes the audio as it
// comes (ignore_volume_control, shairportConf) and each move of the slider sets the device's volume
// instead. Off, as it comes, nothing changes.
//
// The device's volume is not handed back to the phone: AirPlay gives the receiver no way to move the
// sender's slider, so the slider shows where the phone last put it, not where a button or Home
// Assistant moved the device since.

// The ends of the AirPlay slider in decibels. The sender says -144 for the slider at the bottom, which
// is mute.
const (
	airplayLowestDB  = -30.0
	airplayHighestDB = 0.0
)

// airplayStep is the device volume step for the data of a pvol item, "airplay_volume,volume,lowest,
// highest": the first is the slider, -30 to 0 dB, which is spread evenly over the device's steps, 0 to
// media.VolumeSteps. Mute (-144) and anything under the slider's bottom is step 0; the device's own mute
// is left alone. ok is false for data that is not that.
func airplayStep(data string) (step int, ok bool) {
	first, _, _ := strings.Cut(strings.TrimSpace(data), ",")
	db, err := strconv.ParseFloat(strings.TrimSpace(first), 64)
	if err != nil || math.IsNaN(db) || math.IsInf(db, 0) {
		return 0, false
	}
	db = max(airplayLowestDB, min(db, airplayHighestDB))
	return int(math.Round((db - airplayLowestDB) / (airplayHighestDB - airplayLowestDB) * media.VolumeSteps)), true
}

// airplayVolume takes a move of the sender's slider, and sets the device's volume to it when AirPlay
// sets the volume is on. A variable for the tests.
var airplayVolume = func(data string) {
	if !config.Get().Streaming.AirPlaySetsVolume {
		return
	}
	if to, ok := airplayStep(data); ok {
		setAirPlayVolume(to)
	}
}

// setAirPlayVolume moves the device's volume to step. A raise stops where night volume says
// (media.CapRaise), as one from any remote does. A variable for the tests.
var setAirPlayVolume = func(to int) {
	m := media.Get()
	from := m.Volume()
	if to > from {
		to = m.CapRaise(to)
	}
	if to != from {
		slog.Info("airplay: the sender's volume slider sets the volume", "step", to)
		m.Set(to)
	}
}
