package display

import "testing"

// A dark panel keeps the dimmest light while the camera is watched, so its dot is seen; a lit panel
// and an unwatched one are left alone.
func TestAWatchedDarkPanelKeepsALittleLight(t *testing.T) {
	if got := watchedLight(0, false, true, 7); got != 7 {
		t.Errorf("dark and watched: %v, want 7", got)
	}
	if got := watchedLight(0, false, false, 7); got != 0 {
		t.Errorf("dark and not watched: %v, want 0", got)
	}
	if got := watchedLight(40, true, true, 7); got != 40 {
		t.Errorf("lit and watched: %v, want 40", got)
	}
}
