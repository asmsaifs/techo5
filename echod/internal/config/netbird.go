package config

// Netbird is the device on its owner's NetBird network (docs/netbird.md), the other way to reach it from
// away. As with Tailscale, the device's identity there is NetBird's own, kept in its state directory;
// this is whether it runs, and which management server it was joined to.
type Netbird struct {
	// On runs NetBird. Joining turns it on; it stays on until somebody turns it off.
	On bool `json:"on,omitempty"`
	// Management is the management server's URL it was joined with: NetBird's own when empty.
	Management string `json:"management,omitempty"`
	// Joined is whether it has ever been joined: then the screen offers its switch.
	Joined bool `json:"joined,omitempty"`
}

type NetbirdWriter struct{ st *Store }

func (w NetbirdWriter) On(v bool) error {
	return w.st.Update(func(c *Config) { c.Netbird.On = v })
}

// Joined is a join: on, with the management server it was made with.
func (w NetbirdWriter) Joined(management string) error {
	return w.st.Update(func(c *Config) { c.Netbird.On, c.Netbird.Management, c.Netbird.Joined = true, management, true })
}
