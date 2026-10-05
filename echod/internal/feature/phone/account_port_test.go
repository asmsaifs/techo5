package phone

import "testing"

func TestHostPort(t *testing.T) {
	for _, c := range []struct {
		server string
		host   string
		port   int
	}{
		{"chicago1.voip.ms", "chicago1.voip.ms", 5061},
		{"sip.linphone.org:443", "sip.linphone.org", 443},
		{"sip.example.com:0", "sip.example.com:0", 5061},
		{"sip.example.com:x", "sip.example.com:x", 5061},
	} {
		h, p := Account{Server: c.server}.hostPort(5061)
		if h != c.host || p != c.port {
			t.Errorf("%q: got %s %d, want %s %d", c.server, h, p, c.host, c.port)
		}
	}
}
