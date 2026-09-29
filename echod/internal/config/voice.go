package config

import "log/slog"

// Voice is which of the two assistants a wake word or the action button opens a turn on.
//
// One setting, and it is deliberately not a pair of switches that have to agree. Two backends can be
// running at once — a client holding a session open costs a socket and nothing else — and what has
// to be singular is which of them the room is talking to, because both would otherwise open the
// microphone on the same word.
//
// The client has its own switch, Xiaozhi.Enabled, and the two are not the same question. That one
// answers "may this device send microphone audio to a third party at all", which is why it starts
// off and why turning it on is a deliberate act. This one answers "which assistant is this device
// talking to", which is a preference with a working default and nothing dangerous about either end.
type Voice struct {
	Backend VoiceBackend `json:"backend,omitempty"`
}

// VoiceBackend is which assistant answers.
type VoiceBackend string

const (
	// BackendHomeAssistant is the ESPHome satellite, over the connection the device already has. The
	// default on every device, including one that has never heard of xiaozhi: a switch whose default
	// sent a room's audio to a third party would be a switch nobody could leave alone.
	BackendHomeAssistant VoiceBackend = "home_assistant"

	// BackendXiaozhi is the second backend, on its own protocol. Choosing it with nothing else
	// configured is a working path, because the official cloud needs no host, no token and no code
	// beyond the one the device already puts on the screen.
	BackendXiaozhi VoiceBackend = "xiaozhi"
)

// DefaultVoiceBackend is Home Assistant, for the reason on BackendHomeAssistant.
const DefaultVoiceBackend = BackendHomeAssistant

// VoiceBackends is the choice, for a select. Home Assistant first, because it is what the device
// does without being asked.
func VoiceBackends() []VoiceBackend { return []VoiceBackend{BackendHomeAssistant, BackendXiaozhi} }

func (b VoiceBackend) Label() string {
	switch b {
	case BackendXiaozhi:
		return "Xiaozhi"
	case BackendHomeAssistant, "":
		return "Home Assistant"
	}
	return string(b)
}

// Settle is a value this build understands, whatever else might be in the file. A backend stored by
// a build that had a third one settles to Home Assistant rather than leaving a select pointed at an
// option it cannot offer, which Home Assistant renders as a blank row that cannot be changed.
func (b VoiceBackend) Settle() VoiceBackend {
	for _, known := range VoiceBackends() {
		if b == known {
			return b
		}
	}
	if b != "" {
		slog.Warn("no such voice backend, using Home Assistant instead", "asked", b)
	}
	return DefaultVoiceBackend
}

func defaultVoice() Voice { return Voice{Backend: DefaultVoiceBackend} }

type VoiceWriter struct{ st *Store }

func (w VoiceWriter) Backend(v VoiceBackend) error {
	return w.st.Update(func(c *Config) { c.Voice.Backend = v.Settle() })
}
