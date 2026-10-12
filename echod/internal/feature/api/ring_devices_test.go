package api

import (
	"testing"

	"github.com/HuskerMinion/techo5/echod/internal/component"
	"github.com/HuskerMinion/techo5/echod/internal/hardware/led"
)

// The ring's page is offered only where there is a ring: on a Show or a Spot it would be a page of
// controls for nothing.
func TestTheRingPageIsOnlyWhereThereIsARing(t *testing.T) {
	ring := false
	for _, d := range subDevices("Office") {
		if d.ID == component.DeviceRing {
			ring = true
			if d.Name != "Office ring" {
				t.Errorf("the ring's page is named %q", d.Name)
			}
		}
	}
	if ring != led.HasRing {
		t.Errorf("ring page offered %v on a device whose ring is %v", ring, led.HasRing)
	}
}
