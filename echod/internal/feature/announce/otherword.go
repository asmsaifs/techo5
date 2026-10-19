package announce

import (
	"log/slog"
	"net/http"
	"slices"
	"sync"
	"time"
)

// otherWordFor is how long a device that refused us for the house word is still named on the setup
// page, and how long before the log says so again.
const otherWordFor = time.Hour

// otherWordsKept is how many such devices are remembered at once: a house has a few, and a network
// answering under ever new names does not grow the list past this.
const otherWordsKept = 64

// otherWord is the devices that refused what this one sent them as not this house: they have another
// house word, often the same one with other capitals. Each with when it was last seen, and when the
// log last said so.
var otherWord = struct {
	sync.Mutex
	seen, warned map[string]time.Time
}{seen: map[string]time.Time{}, warned: map[string]time.Time{}}

// refusedBy notes a device's answer to something sent it: a 403 is the device saying it has another
// house word.
func refusedBy(name string, status int) {
	if status != http.StatusForbidden {
		return
	}
	now := time.Now()
	otherWord.Lock()
	defer otherWord.Unlock()
	forgetOld(otherWord.seen, now)
	forgetOld(otherWord.warned, now)
	if _, ok := otherWord.seen[name]; !ok && len(otherWord.seen) >= otherWordsKept {
		forgetOldest(otherWord.seen)
	}
	otherWord.seen[name] = now
	if now.Sub(otherWord.warned[name]) >= otherWordFor {
		otherWord.warned[name] = now
		slog.Warn("announce: this device has another house word; check the capitals too", "device", name)
	}
}

// OtherWords is the devices seen with another house word in the last hour, by name.
func OtherWords() []string {
	now := time.Now()
	otherWord.Lock()
	defer otherWord.Unlock()
	var out []string
	for name, at := range otherWord.seen {
		if now.Sub(at) < otherWordFor {
			out = append(out, name)
		} else {
			delete(otherWord.seen, name)
		}
	}
	slices.Sort(out)
	return out
}

// forgetOld drops the devices last seen (or warned of) an hour or more ago.
func forgetOld(m map[string]time.Time, now time.Time) {
	for name, at := range m {
		if now.Sub(at) >= otherWordFor {
			delete(m, name)
		}
	}
}

// forgetOldest drops the device seen longest ago.
func forgetOldest(m map[string]time.Time) {
	oldest, at := "", time.Time{}
	for name, t := range m {
		if oldest == "" || t.Before(at) {
			oldest, at = name, t
		}
	}
	delete(m, oldest)
	delete(otherWord.warned, oldest)
}
