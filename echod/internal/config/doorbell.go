package config

// Doorbell is a doorbell the device rings for (feature/doorbell): one of the cameras on the Reolink
// recorder set up here (Home.Reolink), by entity ("reolink:0"), whose button presses the recorder
// reports over ONVIF on Port. The login is the recorder's. Empty Camera is no doorbell.
//
// Port is the recorder's ONVIF port as the Reolink app shows it; 0 is Reolink's own default.
type Doorbell struct {
	Camera string `json:"camera,omitempty"`
	Port   int    `json:"onvif_port,omitempty"`
}

// DefaultONVIFPort is the port a Reolink serves ONVIF on until somebody changes it.
const DefaultONVIFPort = 8000

// ONVIFPort is Port, or the default where none was given.
func (d Doorbell) ONVIFPort() int {
	if d.Port <= 0 || d.Port > 65535 {
		return DefaultONVIFPort
	}
	return d.Port
}

type DoorbellWriter struct{ st *Store }

// Save sets the doorbell and its port together; an empty camera takes the doorbell away.
func (w DoorbellWriter) Save(camera string, port int) error {
	if port == DefaultONVIFPort || port < 0 || port > 65535 {
		port = 0
	}
	return w.st.Update(func(c *Config) { c.Doorbell = Doorbell{Camera: camera, Port: port} })
}
