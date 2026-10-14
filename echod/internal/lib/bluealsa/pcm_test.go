//go:build linux

package bluealsa

import "testing"

// A stream's level is the louder channel's, a muted channel counts as 0, and volumeOf puts a level on
// both channels.
func TestLevel(t *testing.T) {
	for _, c := range []struct {
		volume uint16
		want   int
	}{
		{0x4747, 71},
		{0x7f7f, 127},
		{0x0000, 0},
		{0x2050, 80},
		{0xc747, 71}, // the left channel muted
		{0xffff, 0},
	} {
		if got := (PCM{Volume: c.volume}).Level(); got != c.want {
			t.Errorf("Level(%#04x) = %d, want %d", c.volume, got, c.want)
		}
	}
	if got := volumeOf(71); got != 0x4747 {
		t.Errorf("volumeOf(71) = %#04x", got)
	}
	if got := volumeOf(500); got != 0x7f7f {
		t.Errorf("volumeOf(500) = %#04x, want the top", got)
	}
}
