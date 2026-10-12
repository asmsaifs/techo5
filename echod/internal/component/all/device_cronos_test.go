//go:build !dot && !spot

package all

import (
	"testing"

	"github.com/HuskerMinion/techo5/echod/internal/component"
)

// notOnThisDevice is what the Echo Show 5 does not put up: it has no LED ring, so nothing that sets
// what a ring shows (led.HasRing).
var notOnThisDevice = ringOnly

// deviceSpecific is what the Show has beyond the shared list: AirPlay and Spotify Connect, which the
// Spot does not offer, and the video player (feature/video), which neither the Spot nor the Dot has.
var deviceSpecific = []string{"airplay", "spotify_connect", "video", "dlna_video", "video_state", "video_title", "video_error"}

// The Show plays videos from Home Assistant: the actions an automation calls are there.
func TestTheShowHasTheVideoActions(t *testing.T) {
	have := map[string]bool{}
	for _, a := range component.Default().Actions() {
		have[a.Name] = true
	}
	for _, name := range []string{"play_video", "stop_video", "pause_video", "resume_video"} {
		if !have[name] {
			t.Errorf("no %s action", name)
		}
	}
}

// actionsNotOnThisDevice: the Show offers every action the shared code has.
var actionsNotOnThisDevice = map[string]string{}
