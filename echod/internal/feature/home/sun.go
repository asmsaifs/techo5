package home

import (
	"log/slog"
	"sync"
	"time"

	"github.com/HuskerMinion/techo5/echod/internal/lib/safe"
	"github.com/HuskerMinion/techo5/echod/internal/lib/sun"
)

// sunKept is today's sunrise and sunset at home. Finding home can ask Home Assistant, which a frame
// being drawn must never wait on, so home's place is looked up off the caller's goroutine, once a day;
// the times themselves are worked out from the last place found, which is quick. A new day with the
// lookup failing still gets that day's times, from where home was yesterday.
var sunKept struct {
	mu        sync.Mutex
	day       string // the day rise and set are for
	rise, set time.Time
	ok        bool

	lat, lon float64
	placed   bool   // lat, lon are known
	looked   string // the day home's place was last looked up
	asking   bool
	retry    time.Time // after a failed lookup, when to try again
}

// SunTimes is today's sunrise and sunset at home, for the Sun clock style and the weather art; ok is
// false until home's place is known, and on a day the sun does not rise or set there.
func (f *Feature) SunTimes(now time.Time) (rise, set time.Time, ok bool) {
	k := &sunKept
	day := now.Format("2006-01-02")
	k.mu.Lock()
	defer k.mu.Unlock()
	if k.looked != day && !k.asking && now.After(k.retry) {
		k.asking = true
		safe.Go("sun times", func() { sunLookUp(day) })
	}
	if !k.placed {
		return time.Time{}, time.Time{}, false
	}
	if k.day != day {
		k.day = day
		k.rise, k.set, k.ok = sun.Times(k.lat, k.lon, now)
	}
	return k.rise, k.set, k.ok
}

// sunLookUp finds home's place, and has the times worked out again from it.
func sunLookUp(day string) {
	k := &sunKept
	defer func() { // a lookup that panicked is tried again later, rather than never
		k.mu.Lock()
		k.asking = false
		k.mu.Unlock()
	}()
	lat, lon, err := homeLocation()
	k.mu.Lock()
	defer k.mu.Unlock()
	if err != nil {
		slog.Debug("sun times wait for home's place", "err", err)
		k.retry = time.Now().Add(10 * time.Minute)
		return
	}
	k.looked = day
	if !k.placed || lat != k.lat || lon != k.lon {
		k.lat, k.lon, k.placed = lat, lon, true
		k.day = "" // worked out again from the new place on the next ask
	}
}
