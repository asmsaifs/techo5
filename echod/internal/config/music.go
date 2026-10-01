package config

// MusicAssistant is a Music Assistant server this device asks for music by voice: "play some Eagles"
// searches it and plays what it finds on this device, which the server knows as a Sendspin player.
// Token is a long-lived token of a Music Assistant user; it is never shown once saved, and a user
// limited to this device's player is the one to make it for.
type MusicAssistant struct {
	URL   string `json:"url,omitempty"`
	Token string `json:"token,omitempty"`
}

// Set is whether there is a server to ask.
func (m MusicAssistant) Set() bool { return m.URL != "" && m.Token != "" }

type MusicAssistantWriter struct{ st *Store }

// Server saves the address, and the token unless token is nil: the setup page leaves a saved token
// alone when its field is left empty.
func (w MusicAssistantWriter) Server(url string, token *string) error {
	return w.st.Update(func(c *Config) {
		c.MusicAssistant.URL = url
		if token != nil {
			c.MusicAssistant.Token = *token
		}
	})
}
