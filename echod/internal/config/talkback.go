package config

// TalkBack is talking through a camera's own speaker (feature/talkback): the device's microphones sent
// to the camera over its RTSP backchannel (ONVIF Profile T), which a Reolink and most cameras with a
// speaker have.
//
// A camera on a Reolink recorder set up here (Home.Reolink) needs nothing: its address and login are
// the recorder's. Any other camera on the list is given its RTSP address in Cameras, by entity, and
// logs in as User and Pass unless the address carries a login of its own. The password is a secret,
// never shown again once saved.
//
// Whether it is allowed at all is Security.TalkBack, which is off on a new device.
type TalkBack struct {
	User    string            `json:"user,omitempty"`
	Pass    string            `json:"pass,omitempty"`
	Cameras map[string]string `json:"cameras,omitempty"`
}

type TalkBackWriter struct{ st *Store }

// Save replaces the login and the addresses together, keeping the saved password when pass is nil:
// the setup page leaves a saved password alone when its field is left empty. An empty address is
// left out.
func (w TalkBackWriter) Save(user string, pass *string, addrs map[string]string) error {
	kept := map[string]string{}
	for e, a := range addrs {
		if e != "" && a != "" {
			kept[e] = a
		}
	}
	if len(kept) == 0 {
		kept = nil
	}
	return w.st.Update(func(c *Config) {
		c.TalkBack.User, c.TalkBack.Cameras = user, kept
		if pass != nil {
			c.TalkBack.Pass = *pass
		}
	})
}
