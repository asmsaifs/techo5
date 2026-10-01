package radiobrowser

import "testing"

// Only stations on the internet are offered: an entry pointing into the home is left out.
func TestOnlyPublicStationsAreKept(t *testing.T) {
	for raw, want := range map[string]bool{
		"https://stream.example.org/live.mp3": true,
		"http://203.0.113.5:8000/stream":      true,
		"http://192.168.1.1/reboot":           false,
		"http://10.0.0.2/":                    false,
		"http://127.0.0.1:8181/":              false,
		"http://[::1]/":                       false,
		"http://router.local/":                false,
		"http://localhost/":                   false,
		"http://user:pw@stream.example.org/":  false,
		"ftp://stream.example.org/x":          false,
		"":                                    false,
	} {
		if got := public(raw); got != want {
			t.Errorf("%q: %v, want %v", raw, got, want)
		}
	}
}
