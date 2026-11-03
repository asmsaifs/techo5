package clap

import "math"

// The detector: two claps, a beat apart and alike, and then quiet.
//
// A clap is a sharp, short, bright sound. Sharp: it starts in one block from about the room's own level to
// far over it, where speech, music and a door closing rise over several. Short: it is down again, well
// under its own peak, within a few blocks. Bright: its energy reaches high up, as a slap of two hands does,
// where a knock on a door, a footstep, a dog's bark or a cup set down are mostly low; the samples' first
// difference measures that, being large for high frequencies and small for low ones. Two of them, as alike
// in loudness and length as two claps by one person are, a beat apart, and nothing after.
//
// The room's level is followed the way the leveler follows it (hardware/mic/level.go): down quickly, up
// slowly, so a clap does not raise it and a room that gets louder for a while is heard as louder.

const (
	// block is how much audio one look takes, in samples at 16 kHz: 10 ms.
	block = 160

	// onsetFrom is how far over the room the block before a clap's start may be at most, in dB: a clap
	// starts from about the room.
	onsetFrom = 6.0

	// clapDrop is how far under its own peak a clap has to fall within clapLongest blocks for it to have
	// been short.
	clapDrop    = 15.0
	clapLongest = 12 // blocks: 120 ms

	// twoApart is the gap between the ends of the two claps, in blocks: 150 to 700 ms, a deliberate double
	// clap and not one clap's echo or a round of applause.
	twoApartMin = 15
	twoApartMax = 70

	// quietAfter is how long after the second clap nothing else clap-like may come, in blocks: 400 ms,
	// so applause and drumming are not taken for two claps.
	quietAfter = 40

	// floorDown and floorUp are how much of the way to a block's level the room's level moves.
	floorDown = 0.3
	floorUp   = 0.002

	// leastFloor is as quiet as the room's level is taken to be, as a block's RMS: below it, the numbers
	// are the converter's own noise.
	leastFloor = 4.0
)

// limits are what it takes to be a double clap, the ones Normal and Strict choose between.
type limits struct {
	// over is how far over the room a block has to be to be a clap's start, in dB.
	over float64

	// bright is the least ratio of the first difference's RMS to the block's RMS for the sound to be a
	// clap's rather than a knock's: about 1.4 for white noise, 0.77 for 2 kHz, 0.2 for 400 Hz.
	bright float64

	// alikeDB is how far apart the two claps' peaks may be, in dB, and alikeBlocks their lengths.
	alikeDB     float64
	alikeBlocks int
}

// normal is the default. strict is for a room where a phone set down or two knocks on a hard desk pass
// (#373): louder over the room, brighter, and the two more alike, since a dropped thing's bounce is
// quieter and shorter than the hit was, where two claps by one person match.
var (
	normal = limits{over: 18, bright: 0.5, alikeDB: 6, alikeBlocks: 4}
	strict = limits{over: 24, bright: 0.8, alikeDB: 3, alikeBlocks: 2}
)

// clap is one heard: how loud at its peak, over the room, how many blocks long, and how bright it started.
type clap struct {
	peak   float64
	blocks int
	bright float64
}

// pair is the double clap last heard, for the event: the two claps and the blocks between their ends.
type pair struct {
	first, second clap
	apart         int
}

// detector looks at the microphone a block at a time.
type detector struct {
	limits

	floor    float64 // the room's level, as RMS
	prevOver float64 // how far over the room the block before was, in dB
	blocks   int

	// The clap being heard: blocks since it started (-1 for none), its peak and how bright it started.
	inClap      int
	peak        float64
	onsetBright float64

	// The claps so far: the first, blocks since it ended, how many, and blocks of quiet since the last.
	first      clap
	sinceFirst int
	claps      int
	quiet      int

	// heard is the double clap add last answered true for.
	heard pair
	next  pair
}

func newDetector(l limits) *detector { return &detector{limits: l, floor: leastFloor, inClap: -1} }

// measure is a block's RMS and how bright it is.
// The block's mean is taken out first: the microphones carry a small DC offset, which would otherwise
// count as loudness.
func measure(b []int16) (rms, brightness float64) {
	var mean float64
	for _, v := range b {
		mean += float64(v)
	}
	mean /= float64(len(b))
	var sum, diff float64
	prev := float64(b[0])
	for _, v := range b {
		x := float64(v)
		sum += (x - mean) * (x - mean)
		d := x - prev
		diff += d * d
		prev = x
	}
	n := float64(len(b))
	rms = math.Sqrt(sum / n)
	if rms > 0 {
		brightness = math.Sqrt(diff/n) / rms
	}
	return rms, brightness
}

// add takes one block of samples and answers whether it ends a double clap.
func (d *detector) add(b []int16) bool {
	if len(b) == 0 {
		return false
	}
	rms, brightness := measure(b)
	if d.blocks == 0 {
		d.floor = math.Max(rms, leastFloor) // the room as first heard, rather than learned up to from nothing
	}
	d.blocks++
	over := 20 * math.Log10(math.Max(rms, 1)/d.floor)
	prevOver := d.prevOver
	d.prevOver = over

	var ended *clap
	switch {
	case d.inClap < 0:
		if over >= d.over && prevOver < onsetFrom && brightness >= d.bright {
			d.inClap, d.peak, d.onsetBright = 0, over, brightness
		}
	default:
		d.inClap++
		d.peak = math.Max(d.peak, over)
		switch {
		case over <= d.peak-clapDrop:
			ended = &clap{peak: d.peak, blocks: d.inClap, bright: d.onsetBright}
			d.inClap = -1
		case d.inClap > clapLongest:
			// Loud for too long: not a clap. Whatever was counted is not a double clap either.
			d.inClap, d.claps = -1, 0
		}
	}

	// The room's level, from blocks that are not a clap.
	if d.inClap < 0 && ended == nil {
		step := floorUp
		if rms < d.floor {
			step = floorDown
		}
		d.floor = math.Max(d.floor+(rms-d.floor)*step, leastFloor)
	}

	if d.claps > 0 {
		d.sinceFirst++
	}
	if ended != nil {
		switch {
		case d.claps == 1 && d.sinceFirst >= twoApartMin && d.sinceFirst <= twoApartMax && d.alike(d.first, *ended):
			d.claps, d.quiet = 2, 0
			d.next = pair{first: d.first, second: *ended, apart: d.sinceFirst}
		default:
			// The first, a third, one too late, too soon or unlike the first: start again from this one.
			d.claps, d.sinceFirst, d.first = 1, 0, *ended
		}
		return false
	}
	if d.claps == 1 && d.sinceFirst > twoApartMax {
		d.claps = 0
	}
	if d.claps == 2 {
		if d.inClap >= 0 {
			d.claps = 0 // something clap-like right after: applause, not a double clap
			return false
		}
		d.quiet++
		if d.quiet >= quietAfter {
			d.claps = 0
			d.heard = d.next
			return true
		}
	}
	return false
}

// alike is whether two claps are as alike as two by one person are.
func (d *detector) alike(a, b clap) bool {
	return math.Abs(a.peak-b.peak) <= d.alikeDB && abs(a.blocks-b.blocks) <= d.alikeBlocks
}

func abs(n int) int {
	if n < 0 {
		return -n
	}
	return n
}

// reset forgets everything but the room's level, for when the device made a sound or was listening.
func (d *detector) reset() { d.inClap, d.claps, d.sinceFirst, d.quiet = -1, 0, 0, 0 }
