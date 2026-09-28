package config

// Xiaozhi is the second voice backend, and the address of the service that answers: who the device
// is to it, and the codec the uplink is encoded with.
//
// The WebSocket is not configured. It comes back from the OTA call every time, and building one out
// of the host instead is the mistake that makes a self-hosted server impossible and an official
// cloud outage unfixable.
type Xiaozhi struct {
	// Enabled holds the client off the network. Off until somebody turns it on: it sends microphone
	// audio to somebody else's servers, which is not something to start doing by itself.
	Enabled bool `json:"enabled,omitempty"`

	// Host is the OTA endpoint's host, empty for the official cloud. A self-hosted server is reached
	// by naming its host here; nothing else about it differs, and the activation step that the
	// official cloud insists on is the only other thing either way.
	Host string `json:"host,omitempty"`

	// Token is what the last OTA call handed back. It is a cache rather than a setting — the next
	// call replaces it — kept so a session can be re-opened without another round trip first.
	Token string `json:"token,omitempty"`

	// ClientID is the device's identity to the cloud, a UUID v4 generated on first use and kept
	// forever after. It is not the MAC: the two are separate, and a device that loses this one is a
	// device that has to be activated again.
	ClientID string `json:"client_id,omitempty"`

	// Activated is whether the code has been redeemed, so a device that has been told does not
	// present one on every boot. The endpoint is the authority; this only says what it last said.
	Activated bool `json:"activated,omitempty"`

	// Bitrate is the uplink Opus bitrate in bits per second, CBR.
	Bitrate int `json:"bitrate,omitempty"`

	// Complexity is how hard the encoder works, 1 to 10. Four was measured on the Show at 3% of a
	// core for the same bitrate as the default's 8.5%, and the mapping is not monotonic below it.
	Complexity int `json:"complexity,omitempty"`

	// FrameMS is the uplink frame duration in milliseconds. Sixty is what the protocol asks for, and
	// it is also the size at which the bitrate is exact: at twenty, VBR overshoots it twofold.
	FrameMS int `json:"frame_ms,omitempty"`
}

const (
	// Off, until somebody turns it on.
	DefaultXiaozhiEnabled = false

	// 24 kbps, 60 ms, complexity 4: the row of the table in docs/xiaozhi-plan.md that is both exact
	// and cheap, and the settings M0 was verified at.
	DefaultXiaozhiBitrate    = 24000
	DefaultXiaozhiComplexity = 4
	DefaultXiaozhiFrameMS    = 60
)

func defaultXiaozhi() Xiaozhi {
	return Xiaozhi{
		Enabled:    DefaultXiaozhiEnabled,
		Bitrate:    DefaultXiaozhiBitrate,
		Complexity: DefaultXiaozhiComplexity,
		FrameMS:    DefaultXiaozhiFrameMS,
	}
}

type XiaozhiWriter struct{ st *Store }

func (w XiaozhiWriter) Enabled(v bool) error {
	return w.st.Update(func(c *Config) { c.Xiaozhi.Enabled = v })
}

func (w XiaozhiWriter) Host(v string) error {
	return w.st.Update(func(c *Config) { c.Xiaozhi.Host = v })
}

func (w XiaozhiWriter) Token(v string) error {
	return w.st.Update(func(c *Config) { c.Xiaozhi.Token = v })
}

func (w XiaozhiWriter) ClientID(v string) error {
	return w.st.Update(func(c *Config) { c.Xiaozhi.ClientID = v })
}

func (w XiaozhiWriter) Activated(v bool) error {
	return w.st.Update(func(c *Config) { c.Xiaozhi.Activated = v })
}

// Frame is how long a frame lasts, in the units a time.Duration wants.
func (x Xiaozhi) Frame() (ms int) {
	if ms = x.FrameMS; ms <= 0 {
		return DefaultXiaozhiFrameMS
	}
	return ms
}
