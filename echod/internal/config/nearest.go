package config

import "time"

// Nearest is how this device takes part in deciding which of several devices that heard the same wake
// word answers it (feature/nearest).
type Nearest struct {
	// On is whether it takes part. Off, it answers every wake word it hears, as before, and Home
	// Assistant keeps whichever device reached it first.
	On bool `json:"on,omitempty"`

	// WindowMs is how long it waits, after its own wake word, to hear from the others. Zero is the
	// default.
	WindowMs int `json:"window_ms,omitempty"`

	// Offset is added to how well it heard the word, in dB, to even out hardware: a device that loses
	// when spoken to right beside it is raised, one that answers from across the room is lowered.
	Offset int `json:"offset,omitempty"`
}

const (
	// DefaultNearestWindowMs covers one device detecting the word a little later than another, and
	// the trip across Wi-Fi, which an access point can hold up for a while.
	DefaultNearestWindowMs = 300
	MinNearestWindowMs     = 100
	MaxNearestWindowMs     = 500

	MinNearestOffset = -30
	MaxNearestOffset = 30
)

// Window is how long to wait to hear from the others.
func (n Nearest) Window() time.Duration {
	ms := n.WindowMs
	if ms == 0 {
		ms = DefaultNearestWindowMs
	}
	return time.Duration(min(max(ms, MinNearestWindowMs), MaxNearestWindowMs)) * time.Millisecond
}

type NearestWriter struct{ st *Store }

func (w NearestWriter) On(v bool) error {
	return w.st.Update(func(c *Config) { c.Nearest.On = v })
}

func (w NearestWriter) WindowMs(ms int) error {
	ms = min(max(ms, MinNearestWindowMs), MaxNearestWindowMs)
	return w.st.Update(func(c *Config) { c.Nearest.WindowMs = ms })
}

func (w NearestWriter) Offset(db int) error {
	db = min(max(db, MinNearestOffset), MaxNearestOffset)
	return w.st.Update(func(c *Config) { c.Nearest.Offset = db })
}
