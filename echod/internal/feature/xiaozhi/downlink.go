package xiaozhi

// The downlink: Opus packets in, speaker frames out.
//
// The server's text to speech arrives as a stream of packets at a rate of its own choosing —
// 24 kHz from the official cloud — and the speaker is a 48 kHz stereo codec. Those are
// different numbers, and reading the wrong one is a chipmunk rather than an error: the
// packets decode either way, they just come out at the wrong speed. So the decoder is built
// at the rate the *speaker* takes, and the number the server said is what decides whether
// its audio is something this build can play at all. libopus converts between all five of
// its rates, so nothing here resamples: the 16 kHz → 48 kHz voice filter in the speaker is
// not touched, and it does not have to become variable-ratio.
//
// What does need care is where the audio lands. The server generates a sentence in bursts
// and sends each burst as it is finished, so laying every packet down where it arrived puts
// a hole between the bursts in the middle of a word. The first packet therefore waits until
// there is a cushion of speech behind it, and everything after that is placed against the
// card's own frame counter — so jitter in arrival moves nothing, and what is paid for that
// is a fixed delay at the head of every answer rather than a variable one.
//
// The decoder is stateful, belongs to the goroutine that reads the socket, and is reset at
// the start of a stretch of speech rather than at the end of the one before: the overlap-add
// and the energy prediction it carries are continuity within a stream, and carrying them
// across a silence of unknown length is how an answer starts with a step.

import (
	"fmt"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"

	"github.com/tphakala/go-opus/opus"

	"github.com/HuskerMinion/techo5/echod/internal/hardware/speaker"
)

const (
	// cushion is how far ahead of the card an answer is laid, and so how much of a gap in
	// the server's own output this device absorbs before the gap becomes a hole in the
	// middle of a sentence.
	//
	// Four 60 ms packets: long enough to cover a burst crossing the network, short enough
	// that an answer does not feel late, and a whole number of frames so the delay is never
	// a fraction of a packet. It is also the gap between the latency the log reports and the
	// latency a person in the room experiences, together with the hardware tail below.
	cushion = 240 * time.Millisecond

	// pcmMax is the longest packet Opus can carry, at the rate this decodes to. The server
	// says 60 ms in its hello and should send it, but a decoder handed a buffer too small
	// for the packet fails on the one packet that does not fit, and this client has no way
	// to ask a server to be consistent.
	pcmMax = speaker.Rate * 120 / 1000

	// sampleMax is full scale, as the loudest sample is reported against it.
	sampleMax = 32768
)

// The two delays this device puts in front of an answer, in output frames: the cushion, and
// the length of time the playback hardware goes on sounding after the last thing handed to
// it. Both are why the first sample is heard later than the first packet arrives.
var (
	cushionFrames = uint64(speaker.Rate * cushion / time.Second)
	tailFrames    = uint64(speaker.Rate * speaker.HardwareTail / time.Second)
)

// downlinkRates are the sample rates the codec can be asked for, and the ones a server's hello
// has to fall inside for its audio to be playable here. A server on 16 kHz is as welcome as one
// on 24, which is the point: the rate is the server's, and this client converts.
var downlinkRates = []int{8000, 12000, 16000, 24000, 48000}

// Speech is one stretch of speech: from the message that began it to the tail the card played,
// and what it cost to hear. It is the log line in full and what a terminal watching is sent when
// one ends, in the same shape as Turn is for the other direction.
type Speech struct {
	// ID numbers the stretches of a whole client, so a terminal watching can tell one ending
	// from the next one starting.
	ID int `json:"id"`

	// State is why it ended. Empty on the way out, where there has only just been a packet.
	State string `json:"state,omitempty"`

	// Packets and Bytes are what was played, and Kbps what that came to — the same measure as
	// the uplink's, read the other way.
	Packets int     `json:"packets"`
	Bytes   int     `json:"bytes"`
	Kbps    float64 `json:"kbps,omitempty"`
	Seconds float64 `json:"seconds,omitempty"`

	// DecodePct is the decoder's share of one core over the time the packets were arriving.
	// The uplink measures its encode against the turn's length and this measures its decode
	// against the answer's, so they are the same kind of number on opposite sides of the same
	// conversation.
	DecodePct float64 `json:"decode_cpu_pct,omitempty"`

	// Latency is how long the cloud took: from the end of the utterance to the first packet of
	// the answer. M0 measured 1.758 s to 2.197 s for the same figure, and that is the service's
	// recognition, agent and speech rather than anything the codec does. What is missing from it
	// is this device's own cushion and the hardware tail, which is the difference between this
	// number and the delay in the room.
	Latency float64 `json:"latency_s,omitempty"`

	// Peak is how loud the answer came out, as a fraction of full scale. A reply that is audible
	// and unintelligible is usually this at zero.
	Peak float64 `json:"peak,omitempty"`

	// Failed, Late and Dropped are the packets that did not happen: refused by the codec, arriving
	// after the card had been where they belonged, and discarded because the device had been told
	// to stop speaking.
	Failed  int `json:"failed,omitempty"`
	Late    int `json:"late,omitempty"`
	Dropped int `json:"dropped,omitempty"`
}

// downlink is the speaking direction for one session: the decoder in front of it, the buffer
// behind, and the speaker's Source that turns that buffer into frames at the right instants.
type downlink struct {
	p   *speaker.Player
	dec *opus.Decoder

	// say is where a finished stretch of speech goes. The feature owns the log and the control
	// socket, so it hands this in rather than this reaching for either.
	say func(*Speech)

	// rate is what the server said it sends, from its own hello. It is not the number the decoder
	// is built from — see the file header — and it is here to be logged and to be the thing a
	// wrong voice is explained by.
	rate int

	// card is the output frame the speaker is about to play. A function so a test can move a card
	// it does not have, which is the reason sendspin's clock is one too.
	card func() uint64

	// ids numbers the stretches of a whole client. It does not reset with the session: a terminal
	// that asked across a reconnect is still watching the one it asked about.
	ids atomic.Int32

	// raw and stereo are decode scratch and belong to the socket's goroutine alone. The decoder is
	// stateful and is only ever fed from there.
	raw    []int16
	stereo []int16

	mu sync.Mutex

	// speaking says an answer is running, and so that the speaker is ours. finished says the
	// server has sent the last packet and only the tail is left. hushed says the device was told
	// to stop and has not been told to start again.
	//
	// They are three booleans because they move at three different moments: an answer runs from
	// its first packet to the end of the tail, a stretch is finished when the server says so,
	// which is before the tail has been heard, and hushed is set by an abort and cleared only by a
	// tts start, so the packets an aborted answer has already in flight do not open a new one.
	speaking, finished, hushed bool

	// anchored is whether the answer has a position yet. pcm holds interleaved stereo from the
	// output frame base onward and ended is the frame after the last of it, which is where the
	// next packet goes: base + len(pcm)/Channels == ended, always, and that is what makes a packet
	// either wholly in time or wholly late and never a third thing.
	anchored bool
	base     uint64
	ended    uint64
	pcm      []int16

	// hold is the cushion: the frames at the head of an answer, not yet placed anywhere, because
	// where the answer begins cannot be decided until there is enough of it in hand to decide
	// with.
	hold []int16

	// played is where the card has got to. A packet that arrives behind it is late, and playing it
	// anyway is what leaves the rest of the answer behind for good.
	played uint64

	// decode is the time in the codec, measured the way the uplink measures its encode, and peak
	// and frames describe what came out of it.
	decode time.Duration
	peak   int16
	frames int

	packets, bytes, failed, late, dropped int

	// id numbers this stretch of speech, opened is when it began, first is when its first packet
	// arrived, and endSpeech is when the last turn ended — the device's end of the utterance, and
	// the instant the latency above is measured from and the one M0 measured the round trip from.
	id                       int
	opened, first, endSpeech time.Time
	latency                  time.Duration
}

var _ speaker.Source = (*downlink)(nil)

// speakerGet is the speaker. A variable so a test can hand over a player of its own: the hardware
// is a piece of hardware, and the source that places audio on it is not.
var speakerGet = speaker.Get

// newDownlink builds the decoder for a session that is open.
//
// rate is what the server's hello said, and it is checked rather than assumed. A rate the codec has
// no answer for is a rate whose audio would come out at the nearest rate it does, which is a
// chipmunk with no log line saying why; so it is refused here, once, in a sentence, and the rate
// the protocol documents is used instead.
func newDownlink(p *speaker.Player, rate int, say func(*Speech)) (*downlink, error) {
	// The speaker's own rate is what the decoder produces, whatever the packet was coded at.
	// Building it from the uplink's 16 kHz instead is the bug this file exists not to have: 16 kHz
	// mono is a third as many samples, and a client that treated the shorter decode as though it
	// were the long one would play every answer at three times the speed with nothing in the log to
	// say so.
	dec, err := opus.NewDecoder(speaker.Rate, 1)
	if err != nil {
		return nil, fmt.Errorf("xiaozhi: the downlink decoder at %d Hz: %w", speaker.Rate, err)
	}

	if !opusRate(rate) {
		slog.Warn("xiaozhi: the server said it would send audio at a rate this build cannot "+
			"decode from, so the rate the protocol documents is assumed instead",
			"server_hz", rate, "assumed_hz", defaultDownlinkRate)
		rate = defaultDownlinkRate
	}

	return &downlink{
		p:      p,
		dec:    dec,
		say:    say,
		rate:   rate,
		card:   p.Written,
		raw:    make([]int16, pcmMax),
		stereo: make([]int16, 0, pcmMax*speaker.Channels),
	}, nil
}

// opusRate is whether the codec has that rate, which is the only question worth asking of a number
// in a hello.
func opusRate(hz int) bool {
	for _, r := range downlinkRates {
		if r == hz {
			return true
		}
	}
	return false
}

// Rate is what the server said it sends, for the log and the status.
func (d *downlink) Rate() int { return d.rate }

// isSpeaking reports whether an answer is running, for the status a person and a terminal read.
func (d *downlink) isSpeaking() bool {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.speaking
}

// started is a tts start: the answer begins.
//
// A tts start in the middle of one is a second sentence and not a second answer, and the difference
// matters. The stream is not reset and the buffer is not thrown away, because the previous
// sentence's tail has not been heard yet, and cutting it is a gap between two words of the same
// reply.
func (d *downlink) started() {
	d.mu.Lock()
	defer d.mu.Unlock()

	// Cleared first and unconditionally: this is the only thing that lets the device speak again
	// after an abort, and that answer's packets are still arriving.
	d.hushed = false

	if d.speaking {
		// A sentence inside the answer already running: only the finish is forgotten, because the
		// tail that was waited for has now been joined by more.
		d.finished = false
		return
	}
	d.open()
}

// open begins a stretch of speech. Wants the lock.
//
// Everything of the last one goes, including the decoder's memory: a new stream has no continuity
// with the one before it, and a decoder that has been silent for a minute holding an overlap-add
// from that minute starts the answer with a step.
func (d *downlink) open() {
	d.speaking, d.finished, d.hushed, d.anchored = true, false, false, false
	d.pcm, d.hold, d.base, d.ended = nil, nil, 0, 0
	d.packets, d.bytes, d.failed, d.late, d.dropped = 0, 0, 0, 0, 0
	d.decode, d.peak, d.frames, d.first, d.latency = 0, 0, 0, time.Time{}, 0
	d.id = int(d.ids.Add(1))
	d.opened = time.Now()

	d.dec.Reset()
	d.p.Attach(d)
}

// stopped is a tts stop: no more packets are coming for this answer.
//
// It is not the end of the sound. The last packet is still a cushion ahead of the card, and the
// speaker is not given back until that has been heard — which is the whole reason the detach is not
// here, because detaching on the message would cut the last quarter of a second off every reply.
//
// It is also the end of the *waiting*, which is the part that is easy to miss. An answer shorter
// than the cushion never fills one, so a client that held the head of an answer until the cushion
// arrived would hold a short acknowledgement forever: the stop is the message that says no more is
// coming, and it is what has to release what there is.
func (d *downlink) stopped() {
	d.mu.Lock()
	defer d.mu.Unlock()

	d.finished = true
	if !d.anchored && len(d.hold) > 0 {
		d.place(d.anchor())
	}
}

// heard is when the last turn ended: the device's end of the utterance, and the instant the latency
// is measured from.
//
// It is kept rather than taken by the caller because a turn can end with no answer ever coming — an
// interrupt, a server that heard nothing — and the next turn's end has to overwrite it either way.
// The answer's own open deliberately leaves this alone: it is a fact about the microphone that
// happened before this stretch of speech began, and clearing it on a tts start would throw the
// measurement away on the ordinary path, where the start is the first thing the server sends.
func (d *downlink) heard(at time.Time) {
	d.mu.Lock()
	d.endSpeech = at
	d.mu.Unlock()
}

// push decodes one packet and lays it where it belongs.
//
// The decode happens outside the lock, on the socket's own goroutine: the codec is stateful, and
// the only thing under the lock is where the samples go. A hushed downlink decodes nothing at all,
// because none of it is going to be played and the decoder is reset when the next answer starts.
func (d *downlink) push(pkt []byte) {
	d.mu.Lock()
	hushed := d.hushed
	if hushed {
		d.dropped++
	}
	d.mu.Unlock()
	if hushed {
		return
	}

	at := time.Now()
	n, err := d.dec.Decode(pkt, d.raw)
	d.mu.Lock()
	d.decode += time.Since(at)
	if err != nil {
		// One bad packet is not a reason to end a session. The server is entitled to send what it
		// likes, and a stream this client cannot read is a sentence missing rather than a client in
		// a loop.
		d.failed++
		failed := d.failed
		d.mu.Unlock()
		if failed == 1 || failed%100 == 0 {
			slog.Warn("xiaozhi: a speech packet would not decode",
				"bytes", len(pkt), "failed", failed, "err", err)
		}
		return
	}
	d.mu.Unlock()

	// Mono to stereo. The codec is one channel whatever the server said it would send; the
	// speaker's source is handed interleaved pairs. It is the hardware that is mono, not the audio.
	out := d.stereo[:0]
	peak := int16(0)
	for _, s := range d.raw[:n] {
		out = append(out, s, s)
		if s < 0 {
			s = -s
		}
		if s > peak {
			peak = s
		}
	}
	d.stereo = out

	d.write(out, len(pkt), peak)
}

// write lays decoded frames at the position the answer's first packet fixed. It is the only writer
// of the buffer and the only place late is decided.
func (d *downlink) write(frames []int16, n int, peak int16) {
	d.mu.Lock()
	defer d.mu.Unlock()

	// Checked again here and not only before the decode. That check let the decode happen outside
	// the lock, and in between a stop can arrive: without this, the packet in hand would be placed
	// into a downlink that had already been hushed, and the `open` below would answer an abort by
	// starting to speak.
	if d.hushed {
		d.dropped++
		return
	}

	if !d.speaking {
		// A server that sends audio without announcing it first is not the protocol. It is also not
		// a reason to play nothing: the packet is playable, so it is played, and the answer is dated
		// from here rather than from a message that never came.
		slog.Debug("xiaozhi: speech began without a tts start", "bytes", n)
		d.open()
	}
	if d.first.IsZero() {
		d.first = time.Now()
		if !d.endSpeech.IsZero() {
			d.latency = d.first.Sub(d.endSpeech)
			d.endSpeech = time.Time{}
		}
	}
	if d.peak < peak {
		d.peak = peak
	}

	// Counted here rather than at the placement, so that packets waiting in the cushion are part of
	// the answer they belong to — an answer too short to fill the cushion is still packets.
	d.packets++
	d.bytes += n

	if !d.anchored {
		d.hold = append(d.hold, frames...)
		if uint64(len(d.hold)/speaker.Channels) < cushionFrames {
			return
		}
		frames = d.anchor()
	}
	d.place(frames)
}

// anchor gives the held head of an answer its position — the card's own next frame, plus the tail
// the hardware keeps sounding for after the last thing handed to it, plus the cushion it is being
// held by — and hands those frames back to be placed. Wants the lock.
//
// Both delays are this device's own, and both are why the latency in the log is smaller than the
// delay a person in the room hears.
func (d *downlink) anchor() []int16 {
	frames := d.hold
	d.hold = nil
	d.base = d.card() + tailFrames + cushionFrames
	d.ended, d.anchored = d.base, true
	return frames
}

// place puts frames at the end of the buffer, or counts them late and drops them. Wants the lock.
//
// The stream is contiguous, so a packet either begins after the card has been or it begins before
// it: there is no third case. One that has been gone is not played, because everything after it
// would then be spoken over the top of what has already been heard.
func (d *downlink) place(frames []int16) {
	if d.ended <= d.played {
		d.late++
		return
	}
	d.pcm = append(d.pcm, frames...)
	d.ended += uint64(len(frames) / speaker.Channels)
	d.frames += len(frames) / speaker.Channels
}

// Render is the speaker asking for the frames it is about to play. It is the write loop's own call:
// at 48 kHz, into a buffer the speaker reuses, on a goroutine that is not the one reading the
// socket.
func (d *downlink) Render(at uint64, out []int16) {
	d.mu.Lock()

	// The card has been here before, and an underrun asks for the same frames twice, so dropping
	// what has gone has to be idempotent rather than a consume.
	if at > d.base {
		// Compared as uint64 and only converted once it is known to fit in the buffer: the card's
		// frame count passes 2^31 samples after about six hours at 48 kHz, and on the 32-bit device
		// an int conversion of it wraps negative and slices out of range.
		if gone := (at - d.base) * speaker.Channels; gone >= uint64(len(d.pcm)) {
			d.pcm = d.pcm[:0]
		} else {
			d.pcm = append(d.pcm[:0], d.pcm[gone:]...)
		}
		d.base = at
	}
	if at > d.played {
		d.played = at
	}

	// The buffer holds frames from base and this call is for frames from at, which is base or
	// before it — never after, because the branch above has already caught up. Zeroed rather than
	// merely copied into: the speaker hands back the same buffer, and a call longer than the audio
	// left in it would otherwise play whatever the last call left behind.
	clear(out)
	if at < d.base {
		// The answer is anchored ahead of the card, which is every call from a tts start until the
		// cushion and the hardware tail have gone by. Leading with silence here is the delay; the
		// alternative is playing the first word before the answer was meant to begin.
		if lead := (d.base - at) * speaker.Channels; lead < uint64(len(out)) {
			copy(out[lead:], d.pcm)
		}
	} else {
		copy(out, d.pcm)
	}

	// The tail is over when the card has been handed it, not when the stop arrived: the stop said
	// the last packet had been sent, and that packet is still a cushion ahead.
	//
	// Every call before that one returns here, and the early return is the point. The speaker is
	// given back on exactly one frame — the one that carries the end of the answer — and a release
	// on any earlier one would end an answer that had only just begun.
	period := uint64(len(out) / speaker.Channels)
	if !d.finished || d.ended > at+period {
		d.mu.Unlock()
		return
	}
	rec := d.record("the tail was heard")
	d.mu.Unlock()

	d.release(rec)
}

// stop cuts the sound off now: an abort, a button, the switch going off.
//
// It is neither a wait nor a fade. Whatever asked wanted the room to go quiet, and the time between
// the request and the quiet is a cushion and a hardware tail — long enough that a caller who needed
// it stopped would be too late to have wanted it stopped at all.
func (d *downlink) stop(why string) {
	d.mu.Lock()
	d.hushed = true
	rec := d.record(why)
	d.mu.Unlock()
	d.release(rec)
}

// close shuts the downlink down, which is what a session ending is.
func (d *downlink) close() { d.stop("the session ended") }

// release gives the speaker back and says how the speech went. It is called off the lock, so a
// terminal is not answered while the write loop is waiting.
func (d *downlink) release(rec *Speech) {
	d.mu.Lock()
	was := d.speaking
	d.speaking, d.finished, d.anchored = false, false, false
	d.pcm, d.hold = nil, nil
	d.mu.Unlock()

	if was {
		d.p.Attach(nil)
	}
	if rec != nil && d.say != nil {
		d.say(rec)
	}
}

// record is how the stretch of speech just ended went. Wants the lock, and answers nil for a stretch
// that never began: a tts stop with nothing said, or a second stop on an answer already released, is
// not a thing to say twice.
//
// The measurement is taken against the time the packets were arriving rather than the length of the
// whole stretch, so the cushion at the head and the tail at the end of it — this device's own
// delays — are not charged to the codec's share of a core.
func (d *downlink) record(why string) *Speech {
	if !d.speaking {
		return nil
	}
	out := &Speech{
		ID: d.id, State: why,
		Packets: d.packets, Bytes: d.bytes, Seconds: round(float64(d.frames) / speaker.Rate),
		Failed: d.failed, Late: d.late, Dropped: d.dropped,
		Peak: round(float64(d.peak) / sampleMax),
	}
	if !d.first.IsZero() {
		if d.latency > 0 {
			out.Latency = round(d.latency.Seconds())
		}
		if took := time.Since(d.first); took > 0 {
			out.Kbps = round(float64(out.Bytes) * 8 / took.Seconds() / 1000)
			out.DecodePct = round(d.decode.Seconds() / took.Seconds() * 100)
		}
	}
	return out
}
