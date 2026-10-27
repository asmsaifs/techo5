//go:build !dot && !panel

package screen

import "testing"

// The Skylight's night range shows every step and is never off; above it, the day is stock's.
func TestSkylightRaw(t *testing.T) {
	for _, top := range []int{BacklightMax, skylightBacklightMax} {
		last := 0
		for level := 1; level <= BacklightMax; level++ {
			raw := skylightRaw(level, top)
			if raw <= 0 || raw > top {
				t.Fatalf("top %d: level %d is %d", top, level, raw)
			}
			if raw < last {
				t.Fatalf("top %d: level %d is %d, under level %d's %d", top, level, raw, level-1, last)
			}
			if level <= skylightNight && raw == last {
				t.Errorf("top %d: night level %d is the same as the one under it (%d)", top, level, raw)
			}
			last = raw
		}
		if skylightRaw(0, top) != 0 {
			t.Errorf("top %d: off is not off", top)
		}
	}
	// The new kernel's duty is the level: the night under stock's floor, the day at stock's duty.
	for level, duty := range map[int]int{1: 1, 20: 20, 128: skylightStockFloor + 128*235/255, 255: 255} {
		if got := skylightRaw(level, BacklightMax); got != duty {
			t.Errorf("level %d: duty %d, want %d", level, got, duty)
		}
	}
	// Stock's table: its raw level is the duty over its floor of 20, as before above the night range.
	if got := skylightRaw(200, skylightBacklightMax); got != 200*235/255 {
		t.Errorf("stock's table, level 200: %d", got)
	}
}
