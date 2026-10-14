package display

import (
	"strings"
	"testing"
	"unicode/utf8"
)

// clipRunes lands where dropping a letter at a time did, for every width a line does not fit, with at
// least the letters asked for.
func TestClipRunesMatchesDroppingALetterAtATime(t *testing.T) {
	slow := func(s string, least int, fits func(string) bool) string {
		rs := []rune(s)
		for len(rs) > least && !fits(string(rs)+"…") {
			rs = rs[:len(rs)-1]
		}
		return string(rs) + "…"
	}
	for _, s := range []string{"A", "Kitchen", "Ünïcödé — näme ✓", strings.Repeat("ab", 30)} {
		for w := 0; w <= utf8.RuneCountInString(s)+2; w++ {
			fits := func(line string) bool { return utf8.RuneCountInString(line) <= w }
			if fits(s) {
				continue // callers clip only a line that does not fit whole
			}
			for _, least := range []int{0, 1} {
				if got, want := clipRunes(s, least, fits), slow(s, least, fits); got != want {
					t.Errorf("clipRunes(%q, %d) at %d = %q, want %q", s, least, w, got, want)
				}
			}
		}
	}
	calls := 0
	clipRunes(strings.Repeat("x", 100000), 1, func(line string) bool { calls++; return len(line) < 50 })
	if calls > 40 {
		t.Errorf("%d measurements for a 100000-letter line", calls)
	}
}
