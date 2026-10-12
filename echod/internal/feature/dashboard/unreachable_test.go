//go:build !dot

package dashboard

import (
	"strings"
	"testing"
)

// A Home Assistant that does not answer is named by where it was looked for, with what to do if it
// moved; with no address, the plain words.
func TestUnreachableSaysWhere(t *testing.T) {
	if got := unreachable("http://192.0.2.10:8123"); !strings.Contains(got, "http://192.0.2.10:8123") || !strings.Contains(got, "home_assistant action") {
		t.Errorf("unreachable = %q", got)
	}
	if got := unreachable(""); got != "Can't reach Home Assistant." {
		t.Errorf("unreachable with no address = %q", got)
	}
}
