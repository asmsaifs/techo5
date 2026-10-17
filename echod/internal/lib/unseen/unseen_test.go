package unseen

import "testing"

func TestRune(t *testing.T) {
	for _, r := range "\x00\t\n\x7f\u202e\u2066\u200f\u061c\u200b\u200d\u2060\ufeff" {
		if !Rune(r) {
			t.Errorf("%U is not hidden", r)
		}
	}
	for _, r := range "aZ0 -'!\u00e9\u00df\u0416\u65e5\u0301" {
		if Rune(r) {
			t.Errorf("%U is hidden", r)
		}
	}
}
