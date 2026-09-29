package xiaozhi

// The uplink: microphone frames in, Opus packets out.
//
// This is the whole of the client's audio in the speaking direction, and it is small because the
// hardware under it is already built. What arrives is the array's own output — mono 16 kHz,
// denoised, gain-controlled and echo cancelled — and what leaves is one binary WebSocket frame per
// packet. Nothing in between resamples, and nothing here touches the capture device.
//
// The encoder is `tphakala/go-opus`: CELT-only, and cgo-free. Each of those is load-bearing, and
// each was settled by a number off the device rather than by reasoning; the table is in
// docs/xiaozhi-plan.md. In short:
//
//   - cgo-free is what keeps techo5 a static binary with no libopus and no libasound. Anything
//     that binds the C library is ruled out for that reason alone.
//   - CELT-only is what go-opus does. The other pure-Go libopus, kazzmir/opus-go, would have given
//     SILK, and it aborts inside the codec on 32-bit armv7: it builds, and it does not run.
//   - constant bitrate at 60 ms is exact. VBR at 20 ms overshoots the requested rate by nearly
//     twofold, which cost a wrong conclusion once and is written down for that reason.
//   - complexity 4 costs 3% of a core where the library default costs 8.5% for the same bitrate.
//
// The microphone is the clock. Three 20 ms frames fill a 60 ms packet and the packets leave as
// fast as the frames arrive, so there is no pacer here to get wrong, and the timing the server is
// sent is the timing the room was heard at.

import (
	"fmt"
	"time"

	"github.com/tphakala/go-opus/opus"

	"github.com/HuskerMinion/techo5/echod/internal/config"
	"github.com/HuskerMinion/techo5/echod/internal/hardware/mic"
)

// packetBuf is the encode buffer. The library says 1276 bytes holds any single-frame packet and
// that a 60 ms frame is three 20 ms sub-frames concatenated, so this is comfortably past all three.
// It matters that it is not short: the encoder adapts its rate to the buffer it is given rather
// than failing, so a tight buffer would quietly cost quality instead of saying so.
const packetBuf = 4096

// opusMaxComplexity is the top of the encoder's effort range. Anything above it is the same
// refusal the library makes, said here where the setting can be corrected.
const opusMaxComplexity = 10

// micListen is the microphone. A variable so a test can hand over frames of its own: the array is
// a piece of hardware, and the encoder underneath it is not.
var micListen = func(name string) (<-chan []int16, func()) { return mic.Get().Listen(name) }

// uplink is the encoder and the buffer in front of it, for one turn.
//
// One per turn. A turn lasts seconds and opening an encoder is the cost of starting one, which is
// nothing against a session that a switch opens and closes.
type uplink struct {
	sess *Session
	enc  *opus.Encoder

	// samples is how many go into one packet, and acc what has arrived of the one being filled.
	// The microphone hands out 20 ms and the protocol wants 60, so there is a remainder after
	// most frames and it carries into the next packet rather than being padded out.
	samples int
	acc     []int16
	pkt     []byte

	// encode is the time spent inside the codec. It is the number the plan's table was measured
	// in, and the only one here that says whether an uplink is affordable on two A53s that are
	// also running the display, the wake word and the canceller.
	encode time.Duration
}

// newUplink builds the encoder the settings ask for.
//
// Every setting falls back to the default rather than to a zero. A device nobody has configured is
// the device M0 was verified on, and a half-configured one should behave like it rather than like
// whatever a zero happens to mean inside the codec.
func newUplink(sess *Session) (*uplink, error) {
	c := config.Get().Xiaozhi
	frame := c.Frame()
	samples := uplinkRate * frame / 1000

	// A frame length Opus does not have, or one the microphone cannot fill a whole number of
	// times, would fail on every single frame with an error from inside the codec. It fails once
	// here instead, in a sentence that names the setting and the alternatives.
	if samples < mic.FrameSamples || samples%mic.FrameSamples != 0 {
		return nil, fmt.Errorf("xiaozhi: a %d ms uplink frame is not one this build can send: "+
			"the encoder has 20, 40, 60, 80, 100 and 120 ms frames, and they have to be a whole "+
			"number of the microphone's %d ms ones", frame, mic.FrameSamples*1000/mic.Rate)
	}

	enc, err := opus.NewEncoder(opus.EncoderConfig{
		SampleRate: uplinkRate,
		Channels:   1,
		Bitrate:    c.Bitrate,
		CBR:        true,
		Complexity: complexity(c.Complexity),
	})
	if err != nil {
		return nil, fmt.Errorf("xiaozhi: the uplink encoder: %w", err)
	}

	return &uplink{
		sess:    sess,
		enc:     enc,
		samples: samples,
		// Room for one packet plus the frame that completes it, so a steady-state turn neither
		// reallocates nor has to compact into a shorter slice.
		acc: make([]int16, 0, samples+mic.FrameSamples),
		pkt: make([]byte, packetBuf),
	}, nil
}

// feed takes what the microphone produced and sends every packet it completed.
//
// A frame that does not complete a packet sends nothing, and that is not an edge case: at 60 ms
// against a 20 ms microphone there is a remainder after every second frame. What is left when a
// turn ends is dropped rather than flushed — Opus has no way to say "this packet is short", and a
// third of a packet sent as a whole one is a click in the recognizer's ear.
func (u *uplink) feed(frame []int16) error {
	u.acc = append(u.acc, frame...)

	for len(u.acc) >= u.samples {
		at := time.Now()
		n, err := u.enc.Encode(u.acc[:u.samples], u.pkt)
		u.encode += time.Since(at)
		if err != nil {
			return fmt.Errorf("xiaozhi: encoding the uplink: %w", err)
		}
		if err := u.sess.Audio(u.pkt[:n]); err != nil {
			return err
		}
		// Compacted in place rather than resliced off the front: the buffer is allocated once and
		// a turn can run for a minute, so this is the difference between an uplink that allocates
		// every frame and one that never does.
		u.acc = u.acc[:copy(u.acc, u.acc[u.samples:])]
	}
	return nil
}

// pending is how much of a packet has not gone out yet, in samples.
//
// It is the trailing audio a turn drops, and the reason a turn that ends between packets sounds
// like it was cut: the endpoint that ends a turn knows where the speech stopped, but by then the
// frames past it are already on the wire.
func (u *uplink) pending() int { return len(u.acc) }

// complexity is the encoder's effort, 1 to 10, from the setting.
//
// Out of range falls back to the measured default rather than to the library's 10, which is the
// setting the plan measured at 3% against and which costs two and a half times the CPU for the
// same bitrate.
func complexity(v int) int {
	if v < 1 || v > opusMaxComplexity {
		return config.DefaultXiaozhiComplexity
	}
	return v
}
