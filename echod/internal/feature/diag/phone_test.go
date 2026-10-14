package diag

import (
	"testing"

	"github.com/HuskerMinion/techo5/echod/internal/feature/media"
)

// Only a phone over Bluetooth is a name to leave out of the bundle, not an app's.
func TestPhonePlaying(t *testing.T) {
	if name, ok := phonePlaying(media.BluetoothPrefix + "Phone A"); !ok || name != "Phone A" {
		t.Errorf("a phone: %q %v", name, ok)
	}
	for _, app := range []string{"AirPlay", "Spotify", "DLNA", ""} {
		if _, ok := phonePlaying(app); ok {
			t.Errorf("%q taken for a phone", app)
		}
	}
}
