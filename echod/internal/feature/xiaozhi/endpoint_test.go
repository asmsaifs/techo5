package xiaozhi

import (
	"github.com/HuskerMinion/techo5/echod/internal/lib/endpoint"
	"testing"
)

// Synthetic envelope for "Computer, hôm nay ngày mấy?" after wake handoff.
// This tests timing, not wake recognition or Vietnamese transcription.
func TestManualRequestSurvivesPauses(t *testing.T) {
	for _, pause := range []int{3, 5, 8} {
		d := endpoint.New(manualEndpointSettings())
		speech := make([]int16, endpoint.Window)
		for i := range speech {
			speech[i] = 1800
		}
		quiet := make([]int16, endpoint.Window)
		for range 5 {
			if d.Feed(speech) {
				t.Fatal("ended during first phrase")
			}
		}
		for range pause {
			if d.Feed(quiet) {
				t.Fatalf("ended during %d00 ms pause", pause)
			}
		}
		for range 100 {
			if d.Feed(speech) {
				t.Fatal("ended during long question")
			}
		}
		for range 11 {
			if d.Feed(quiet) {
				t.Fatal("ended before final silence threshold")
			}
		}
		if !d.Feed(quiet) {
			t.Fatal("did not end after final silence")
		}
		if got, want := d.EndedAt(), (105+pause)*endpoint.Window; got != want {
			t.Fatalf("end sample %d, want %d", got, want)
		}
	}
}
