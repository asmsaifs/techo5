package config

// Streaming is the device as a speaker other apps play to (feature/streaming): AirPlay from an
// iPhone, iPad or Mac, and Spotify Connect from the Spotify app. Both are off on a new device: each
// runs a program that listens on the network while it is on.
type Streaming struct {
	AirPlay bool `json:"airplay,omitempty"`
	Spotify bool `json:"spotify,omitempty"`
}

type StreamingWriter struct{ st *Store }

func (w StreamingWriter) AirPlay(v bool) error {
	return w.st.Update(func(c *Config) { c.Streaming.AirPlay = v })
}

func (w StreamingWriter) Spotify(v bool) error {
	return w.st.Update(func(c *Config) { c.Streaming.Spotify = v })
}
