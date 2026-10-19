package diag

import (
	"slices"
	"testing"

	"github.com/HuskerMinion/techo5/echod/internal/config"
)

// The devices sharing names in its log are among those taken out of the bundle.
func TestOtherDevicesAreKnown(t *testing.T) {
	c := config.Defaults()
	c.Share.Links = map[string][]string{"bass": {"Office"}}
	c.Share.Waiting = map[string][]string{"bass": {"Den"}}
	c.Share.From = map[string]config.ShareFrom{"bass": {Device: "Porch"}}
	c.Share.Requests = []config.ShareRequest{{ID: "bass", Device: "Attic"}}
	c.Share.Declined = []config.ShareRequest{{ID: "bass", Device: "Garage"}}
	got := otherDevices(c)
	for _, want := range []string{"Office", "Den", "Porch", "Attic", "Garage"} {
		if !slices.Contains(got, want) {
			t.Errorf("%s not known: %v", want, got)
		}
	}
}

// A device named with a common word is not taken out of the whole bundle.
func TestCommonNamesAreLeft(t *testing.T) {
	for _, n := range []string{"Show", "spot", "Den"} {
		if !commonName(n) {
			t.Errorf("%s would be taken out everywhere", n)
		}
	}
	if commonName("Office") {
		t.Error("Office left in")
	}
}
