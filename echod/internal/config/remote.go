package config

// Remote is the remote API (feature/remote): whether it is on, and what checks a request is signed by the
// key that was shown once on the setup page.
type Remote struct {
	On bool `json:"on,omitempty"`

	// Secret is the key's signing secret (remote.SecretOf), hex: what verifies a signature, not the key
	// itself, which is never kept. Empty while no key has been made.
	Secret string `json:"secret,omitempty"`
}

type RemoteWriter struct{ st *Store }

func (w RemoteWriter) On(v bool) error {
	return w.st.Update(func(c *Config) { c.Remote.On = v })
}

func (w RemoteWriter) Secret(v string) error {
	return w.st.Update(func(c *Config) { c.Remote.Secret = v })
}
