//go:build spot

package bluetooth

import (
	"time"

	"github.com/HuskerMinion/techo5/echod/internal/hardware/speaker"
)

// The Spot's Wi-Fi and Bluetooth share one chip, and BlueZ's discovery scans without a break and
// reports every advertisement it hears: with the proxy on, what the Spot played broke up
// (techo5-spot#10), where the Dots, scanning their own way, play on. So while the speaker plays, the
// proxy scans a quarter of the time, a second in every four. A connection being made pauses the
// scanner too; the pauses nest, so neither undoes the other.
const (
	playScan = time.Second
	playRest = 3 * time.Second
)

// restWhilePlaying runs for as long as the daemon does.
func (b *Proxy) restWhilePlaying() {
	if b.links.pause == nil {
		return
	}
	rest(nil, b.links.pause, playScan, playRest, func() bool { return b.Enabled() && playing() })
}

// rest pauses the scanner for restFor after every scanFor that wanted says it should, until stop closes,
// which only a test does.
func rest(stop <-chan struct{}, pause func() (resume func()), scanFor, restFor time.Duration, wanted func() bool) {
	for {
		select {
		case <-stop:
			return
		case <-time.After(scanFor):
		}
		if !wanted() {
			continue
		}
		resume := pause()
		select {
		case <-stop:
		case <-time.After(restFor):
		}
		resume()
	}
}

// playing is whether the speaker plays anything: a reply, a sound, music.
func playing() bool {
	s := speaker.Sound()
	return s.Busy() || s.Backgrounds().Playing() != nil
}
