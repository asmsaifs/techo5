//go:build !dot && !spot

package display

import (
	"image"
	"strings"
	"testing"
	"time"
)

// clipTo halves its way to the longest start that fits, and lands where dropping a letter at a time
// did; a name thousands of letters long is clipped at once.
func TestClipToMatchesDroppingALetterAtATime(t *testing.T) {
	r := newRenderer(image.NewRGBA(image.Rect(0, 0, 480, 240)))
	slow := func(s string, w int) string {
		if r.width(r.small, s) <= w {
			return s
		}
		rs := []rune(s)
		for len(rs) > 1 && r.width(r.small, string(rs)+"…") > w {
			rs = rs[:len(rs)-1]
		}
		return string(rs) + "…"
	}
	for _, s := range []string{"", "A", "Kitchen", "The Very Long Name Of A Station Somewhere", "Ünïcödé — näme ✓", strings.Repeat("Wm", 40)} {
		for _, w := range []int{0, 5, 20, 60, 120, 300, 1000} {
			if got, want := r.clipTo(r.small, s, w), slow(s, w); got != want {
				t.Errorf("clipTo(%q, %d) = %q, want %q", s, w, got, want)
			}
		}
	}
	long := strings.Repeat("Robo", 5000)
	start := time.Now()
	if got := r.clipTo(r.small, long, 300); !strings.HasSuffix(got, "…") || len(got) > 200 {
		t.Errorf("long name clipped to %q", got)
	}
	if d := time.Since(start); d > 2*time.Second {
		t.Errorf("clipping a 20000-letter name took %v", d)
	}
}
