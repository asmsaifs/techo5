package config

// Notifications are a phone's notifications shown on the screen, as Home Assistant's companion app
// reports them (feature/notification, docs/phone-notifications-plan.md).
type Notifications struct {
	// Entities are the phones' Last notification sensors. None turns it off.
	Entities []string `json:"entities,omitempty"`

	// ShowText puts what a notification says on the screen, not only who it is from. Off by default:
	// the screen is the room's, and a message or a login code is not.
	ShowText bool `json:"show_text,omitempty"`

	// Sound is what a new notification sounds like, by name (speaker.NotificationSounds); empty is
	// the first. Quiet hours leave it out: it is the device speaking up by itself.
	Sound string `json:"sound,omitempty"`
}

type NotificationsWriter struct{ st *Store }

func (w NotificationsWriter) Entities(v []string) error {
	return w.st.Update(func(c *Config) { c.Notifications.Entities = v })
}

func (w NotificationsWriter) ShowText(v bool) error {
	return w.st.Update(func(c *Config) { c.Notifications.ShowText = v })
}

func (w NotificationsWriter) Sound(v string) error {
	return w.st.Update(func(c *Config) { c.Notifications.Sound = v })
}
