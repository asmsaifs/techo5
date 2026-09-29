package xiaozhi

// The downlink: the server's Opus, decoded at the speaker's rate, laid where the card will be when
// it gets there, and given back when the tail has been heard.
//
// The three things that can be got wrong here are all silent in a way a plain "did audio come out"
// check would not catch. A decoder built from the uplink's rate plays everything three times too
// fast — the samples are there, the count is plausible, and the answer is a chipmunk. Laying
// packets where they arrive instead of against the card puts a hole in the middle of every word
// the server generated in more than one burst. Detaching the speaker on a tts stop cuts the last
// quarter of a second off every reply. So the checks are on the rate the samples come out at, on
// the frame index they land at, and on when the speaker is handed back.

import (
	"math"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/tphakala/go-opus/opus"

	"github.com/HuskerMinion/techo5/echod/internal/hardware/speaker"
)

// speakerPeriod is the block the speaker's write loop asks for, so a test that wants to look like
// one asks in the same size.
const speakerPeriod = 480

// primed is a few milliseconds of slack for the codec's own startup. A stream does not begin at
// full amplitude on its first decoded frame — the overlap-add and the energy prediction behind it
// have nothing to work from yet — so the first frame that is not exact silence is a little after
// where the audio was placed. It is the codec's delay and not this code's, and it is why the spans
// below are compared with a margin: stated once, here, rather than fudged per assertion.
const primed = 300

// answer encodes a tone into the packets a server would send: the same length and rate the official
// cloud uses, which is what makes a decode assertion a decode assertion rather than a count of
// slices.
func answer(t *testing.T, rate, frameMS, packets int, hz float64) [][]byte {
	t.Helper()

	enc, err := opus.NewEncoder(opus.EncoderConfig{
		SampleRate: rate, Channels: 1, Bitrate: 24000, CBR: true, Complexity: 4,
	})
	if err != nil {
		t.Fatalf("building an encoder at %d Hz: %v", rate, err)
	}

	n := rate * frameMS / 1000
	pcm := make([]int16, n)
	buf := make([]byte, 1500)

	// Started at a quarter cycle rather than at zero, so the tone begins at full amplitude. A sine
	// that began at zero spends its first quarter cycle below what an int16 can hold, and the
	// decoded start of it is a run of exact zeros — which makes the first non-silent output frame a
	// measurement of the quantiser rather than of where the audio was placed.
	phase := math.Pi / 2

	out := make([][]byte, 0, packets)
	for p := range packets {
		for i := range pcm {
			pcm[i] = int16(12000 * math.Sin(phase))
			phase += 2 * math.Pi * hz / float64(rate)
		}
		got, err := enc.Encode(pcm, buf)
		if err != nil {
			t.Fatalf("encoding packet %d: %v", p, err)
		}
		out = append(out, append([]byte(nil), buf[:got]...))
	}
	return out
}

// bench is a downlink with a card a test moves by hand. There is no hardware in a test, and Written
// only moves when there is, so the one number the placement is measured against has to be the
// test's own.
type bench struct {
	d    *downlink
	p    *speaker.Player
	card atomic.Uint64

	mu   sync.Mutex
	said []*Speech
}

func newBench(t *testing.T, rate int) *bench {
	t.Helper()

	b := &bench{p: speaker.New()}
	d, err := newDownlink(b.p, rate, func(s *Speech) {
		b.mu.Lock()
		b.said = append(b.said, s)
		b.mu.Unlock()
	})
	if err != nil {
		t.Fatalf("newDownlink(%d): %v", rate, err)
	}
	d.card = func() uint64 { return b.card.Load() }
	b.d = d
	return b
}

// render hands the card n frames and returns what it was given, in order.
func (b *bench) render(n int) []int16 {
	blk := make([]int16, speakerPeriod*speaker.Channels)
	out := make([]int16, 0, n*speaker.Channels)
	for done := 0; done < n; done += speakerPeriod {
		b.d.Render(b.card.Load(), blk)
		b.card.Add(speakerPeriod)
		out = append(out, blk...)
	}
	return out
}

// drain renders until the downlink lets the speaker go, or fails if it never does. The cap is a
// whole answer plus the cushion and the tail with room to spare, so a downlink that hung on would
// fail here rather than loop forever.
func (b *bench) drain(t *testing.T) []int16 {
	t.Helper()

	out := []int16{}
	for i := 0; i < 500 && b.d.isSpeaking(); i++ {
		out = append(out, b.render(speakerPeriod)...)
	}
	if b.d.isSpeaking() {
		t.Fatal("the downlink never gave the speaker back")
	}
	return out
}

func (b *bench) speeches() []*Speech {
	b.mu.Lock()
	defer b.mu.Unlock()
	return append([]*Speech(nil), b.said...)
}

// span is the first and last output frame index that was not silence. Decoded tone never lands on
// exactly zero, and a sine crosses it, so the ends of the span are the ends of the audio.
func span(out []int16) (int, int) {
	lo, hi := -1, -1
	for i := 0; i < len(out); i += speaker.Channels {
		if out[i] != 0 {
			if lo < 0 {
				lo = i
			}
			hi = i
		}
	}
	return lo / speaker.Channels, hi / speaker.Channels
}

// pitch is the frequency of what was played, from its positive-going zero crossings. It is a
// cruder measure than a transform and the right one here: the failure being looked for is a tone
// at the wrong speed, and a rate three times too high is three times as many crossings.
//
// The span is in frames, not in interleaved samples as the indices span returns are, and getting
// that wrong is a factor of two — uncomfortably close to the factor of three being looked for.
func pitch(out []int16, rate int) float64 {
	lo, hi := span(out)
	if lo < 0 || hi <= lo {
		return 0
	}
	first := lo * speaker.Channels
	last := hi * speaker.Channels

	crossings := 0
	prev := out[first]
	for i := first; i <= last; i += speaker.Channels {
		if prev <= 0 && out[i] > 0 {
			crossings++
		}
		prev = out[i]
	}
	return float64(crossings) * float64(rate) / float64(hi-lo)
}

// The plan's central risk for the downlink. The server sends 24 kHz and the speaker takes 48 kHz,
// and those are different numbers: a decoder built from the rate this client *sends* — the mistake
// the code's own comments warn about — decodes a 60 ms packet into 960 samples instead of 2880, and
// the speaker plays those 960 at 48 kHz in 20 ms. Every answer comes out at three times the speed,
// the packet count is unchanged, and nothing logs a thing. So the count is checked, and then the
// pitch is checked, because a wrong resample would pass the first and fail the second.
func TestTheDownlinkDecodesAtTheSpeakersRateNotTheUplinks(t *testing.T) {
	const packets, frameMS = 25, 60
	b := newBench(t, defaultDownlinkRate)

	for _, pkt := range answer(t, defaultDownlinkRate, frameMS, packets, 440) {
		b.d.push(pkt)
	}
	b.d.stopped()
	out := b.drain(t)

	want := packets * speaker.Rate * frameMS / 1000
	slow := packets * uplinkRate * frameMS / 1000
	if b.d.frames != want {
		t.Errorf("the downlink produced %d output frames, want %d (%d would be a decoder built "+
			"from the uplink's %d Hz, which is the chipmunk)", b.d.frames, want, slow, uplinkRate)
	}
	if got := pitch(out, speaker.Rate); math.Abs(got-440) > 10 {
		t.Errorf("the answer played at %.0f Hz, want 440: the decode rate is wrong, not the count", got)
	}
}

// A whole answer is laid at one fixed position ahead of the card, and that position is the cushion
// plus the tail the hardware keeps sounding. Both are this device's own delay and both are the
// reason nothing arrives in the middle of a sentence.
func TestTheAnswerIsLaidACushionAndTheHardwareTailAheadOfTheCard(t *testing.T) {
	b := newBench(t, defaultDownlinkRate)

	// Four packets fill the cushion exactly, so the answer is anchored by the fourth and the first
	// three have been held rather than placed.
	pkts := answer(t, defaultDownlinkRate, 60, 4, 440)
	for _, pkt := range pkts {
		b.d.push(pkt)
	}
	b.d.stopped()

	if !b.d.anchored {
		t.Fatal("four 60 ms packets did not fill a 240 ms cushion")
	}
	want := int(cushionFrames + tailFrames)
	if got := int(b.d.base); got != want {
		t.Errorf("the answer was anchored at frame %d, want %d (a %s cushion and a %s tail)",
			got, want, cushion, speaker.HardwareTail)
	}

	out := b.drain(t)
	lo, hi := span(out)
	if lo < want {
		t.Errorf("the first thing played was at output frame %d, before the anchor at %d: audio "+
			"was placed ahead of where the answer begins", lo, want)
	}
	if lo > want+primed {
		t.Errorf("the first thing played was at output frame %d, want %d: the answer is being "+
			"delayed by more than the cushion and the tail", lo, want)
	}
	if got := hi - lo + 1; got < 4*2880-primed {
		t.Errorf("only %d frames of the answer were played, want about %d", got, 4*2880)
	}
}

// Three packets do not fill a cushion, so a server that stops there has produced nothing this
// device should play yet — and the answer still has to come out, from the held frames, when the
// cushion is topped up.
func TestAnAnswerShorterThanTheCushionStillPlays(t *testing.T) {
	b := newBench(t, defaultDownlinkRate)

	// The server's own audio is shorter than the cushion it is being held by, which a short
	// acknowledgement really is.
	for _, pkt := range answer(t, defaultDownlinkRate, 60, 3, 440) {
		b.d.push(pkt)
	}
	if b.d.anchored {
		t.Error("three 60 ms packets anchored a 240 ms cushion")
	}
	b.d.stopped()

	// The stop is the last thing that will arrive, so the cushion has to be released by it, not
	// held forever waiting for audio that is not coming.
	out := b.drain(t)
	if _, hi := span(out); hi < 0 {
		t.Fatal("an answer shorter than the cushion played nothing at all")
	}
	if b.d.frames == 0 {
		t.Error("the held frames were never placed")
	}
}

// The tts stop is where the speaker would be given back by a client that read the message and
// thought it meant the sound. It does not: the last packet is a cushion ahead of the card, and
// detaching here cuts the last quarter of a second off every reply. The stop ends the *stream*; the
// tail ends the sound.
func TestATtsStopDoesNotCutTheLastOfTheAnswer(t *testing.T) {
	const packets = 8
	b := newBench(t, defaultDownlinkRate)

	for _, pkt := range answer(t, defaultDownlinkRate, 60, packets, 440) {
		b.d.push(pkt)
	}
	b.d.stopped()

	if !b.d.isSpeaking() {
		t.Fatal("the tts stop ended the answer before any of the tail had been heard")
	}
	out := b.drain(t)

	if got, want := b.d.frames, packets*2880; got != want {
		t.Errorf("%d frames were played, want %d: the stop cut the answer", got, want)
	}
	lo, hi := span(out)
	if hi < 0 {
		t.Fatal("nothing was played")
	}
	// A stop that was read as the end of the sound would drop the last cushion's worth — 11520
	// frames — so a span within one codec priming of the whole answer is the check that it did not.
	if got := hi - lo + 1; got < packets*2880-primed {
		t.Errorf("the played span was %d frames, want about %d: the stop cut the answer",
			got, packets*2880)
	}

	said := b.speeches()
	if len(said) != 1 {
		t.Fatalf("%d speech records for one answer, want 1", len(said))
	}
	if said[0].State != "the tail was heard" {
		t.Errorf("the speech ended as %q, want the tail being heard", said[0].State)
	}
	if said[0].Packets != packets {
		t.Errorf("the record says %d packets, want %d", said[0].Packets, packets)
	}
	if b.d.isSpeaking() {
		t.Error("the answer is still marked as running after the tail was heard")
	}
}

// An abort is the one thing that is meant to cut the sound off, and it has to work when there is no
// turn behind it — which is most of the time an abort is worth having, because the microphone
// finished sending seconds ago and the cloud is still talking.
func TestAnAbortSilencesTheAnswerAndIgnoresWhatWasInFlight(t *testing.T) {
	b := newBench(t, defaultDownlinkRate)
	pkts := answer(t, defaultDownlinkRate, 60, 4, 440)
	for _, pkt := range pkts {
		b.d.push(pkt)
	}
	if !b.d.isSpeaking() {
		t.Fatal("the answer was not running before it was interrupted")
	}

	b.d.stop("it was interrupted")

	if b.d.isSpeaking() {
		t.Error("the abort left the answer running")
	}
	said := b.speeches()
	if len(said) != 1 || said[0].State != "it was interrupted" {
		t.Fatalf("%d speech records after an abort, want 1 saying why", len(said))
	}

	// The packets already in flight when the abort went out are the ones that would reopen it. They
	// are dropped rather than decoded: there is no point decoding audio that cannot be played, and
	// resetting the codec is what the next answer does anyway.
	dropped, frames := b.d.dropped, b.d.frames
	for _, pkt := range pkts {
		b.d.push(pkt)
	}
	if b.d.dropped <= dropped {
		t.Error("a packet that arrived after the abort was not dropped")
	}
	if b.d.frames != frames {
		t.Error("a packet that arrived after the abort was decoded and placed")
	}
	if b.d.isSpeaking() {
		t.Error("a packet that arrived after the abort reopened the answer")
	}

	// Only a tts start lifts the hush. Anything else would let an aborted answer's leftovers open a
	// new one, which is the device speaking when it was told to be quiet.
	b.d.started()
	if !b.d.isSpeaking() {
		t.Fatal("a tts start after an abort did not begin a new answer")
	}
	b.d.push(pkts[0])
	if b.d.packets == 0 {
		t.Error("the packet after a new tts start was still being dropped")
	}
}

// The card can be slower than the cloud, and when it is, a packet arrives for a frame that has
// already been played. Playing it anyway puts everything after it on top of what has already been
// heard, so it is counted and dropped instead.
func TestAPacketThatReachesTheCardLateIsNotPlayedBehindIt(t *testing.T) {
	b := newBench(t, defaultDownlinkRate)

	// The cushion is exactly four 60 ms packets, so the fourth is the one that anchors — the first
	// three are held, not placed.
	full := answer(t, defaultDownlinkRate, 60, 5, 440)
	for _, pkt := range full[:3] {
		b.d.push(pkt)
	}
	if b.d.anchored {
		t.Fatal("the answer anchored before the cushion was full")
	}
	b.d.push(full[3])
	if !b.d.anchored {
		t.Fatal("the packet that filled the cushion did not anchor the answer")
	}

	// The card runs past everything laid down, and only then does the next packet arrive. The card
	// has to be moved through Render and not by the frame counter alone: it is the render that
	// records where the card has been, and the late check is against that.
	b.card.Store(b.d.ended + 1000)
	b.render(speakerPeriod)

	frames, late := b.d.frames, b.d.late
	b.d.push(full[4])

	if b.d.late <= late {
		t.Error("a packet for a frame the card had passed was not counted as late")
	}
	if b.d.frames != frames {
		t.Error("a late packet was placed behind the card, over what had already been heard")
	}
}

// Two sentences of one answer are one answer. The server sends a tts stop between them, and that is
// the message a client would hand the speaker back on — so the second sentence's start arrives while
// the first is still being heard, and the two have to join without a hole or a second record.
func TestTwoSentencesOfOneAnswerAreOneAnswer(t *testing.T) {
	b := newBench(t, defaultDownlinkRate)

	b.d.started()
	for _, pkt := range answer(t, defaultDownlinkRate, 60, 5, 440) {
		b.d.push(pkt)
	}
	b.d.stopped()
	if !b.d.isSpeaking() {
		t.Fatal("the first sentence let the speaker go before its tail was heard")
	}

	// The second sentence, while the first is still playing.
	b.d.started()
	for _, pkt := range answer(t, defaultDownlinkRate, 60, 5, 660) {
		b.d.push(pkt)
	}
	b.d.stopped()
	b.drain(t)

	if got := len(b.speeches()); got != 1 {
		t.Errorf("two sentences of one answer made %d speech records, want 1", got)
	}
	if got, want := b.d.frames, 10*2880; got != want {
		t.Errorf("%d frames were played, want %d: a sentence was cut or dropped", got, want)
	}
}

// A session that ends stops the sound, and the id numbering survives it: a terminal that asked for
// a speech across a reconnect is still watching the one it asked about.
func TestClosingTheDownlinkEndsTheAnswer(t *testing.T) {
	b := newBench(t, defaultDownlinkRate)
	for _, pkt := range answer(t, defaultDownlinkRate, 60, 4, 440) {
		b.d.push(pkt)
	}
	first := b.d.id

	b.d.close()
	if b.d.isSpeaking() {
		t.Error("closing the downlink left the answer running")
	}
	said := b.speeches()
	if len(said) != 1 || said[0].State != "the session ended" {
		t.Fatalf("%d speech records after a close, want 1 saying why", len(said))
	}

	if b.d.id != first {
		t.Error("closing the downlink renumbered the speech")
	}
}

// The whole path, over a real socket: the server sends a tts start, binary packets and a tts stop,
// the session's read loop hands each binary frame to the downlink with its payload, the downlink
// decodes it, and what comes out of the speaker is the tone that went in.
//
// The unit tests above drive the downlink directly, which is what makes them able to say exactly
// where a frame lands. This one is the opposite: nothing is stubbed but the card, and the point is
// that a packet survives the trip from a binary WebSocket frame to a decoded sample at all — which
// is the part that has four layers between the two.
func TestTheServersSpeechIsDecodedAndPlayedOverTheSocket(t *testing.T) {
	c := connect(t)

	// Eight packets is a real answer, but not enough of one to measure a pitch from: the codec's
	// warm-up adds a handful of spurious zero crossings at the head, and over half a second those
	// are four percent of the total. A second and a half is what the unit test uses and enough for
	// the transients to be noise.
	const packets, frameMS = 25, 60
	c.cloud.speak(t, answer(t, defaultDownlinkRate, frameMS, packets, 440))

	down := c.downlink(t)
	waitDecoded(t, down, packets*speaker.Rate*frameMS/1000)

	down.mu.Lock()
	got, stopped, rate := down.packets, down.finished, down.rate
	down.mu.Unlock()

	if got != packets {
		t.Errorf("the downlink decoded %d of %d packets", got, packets)
	}
	if !stopped {
		t.Error("the tts stop did not reach the downlink")
	}
	if rate != defaultDownlinkRate {
		t.Errorf("the downlink is built for a server sending %d Hz, want %d", rate, defaultDownlinkRate)
	}

	// There is no card here, so the card is driven by hand. Written only moves when the speaker is
	// actually running, and what is being tested is where the downlink puts its frames, not the
	// driver.
	var card atomic.Uint64
	down.mu.Lock()
	down.card = func() uint64 { return card.Load() }
	down.mu.Unlock()

	budget := packets*speaker.Rate*frameMS/1000 + int(cushionFrames+tailFrames) + speakerPeriod
	out := drive(t, down, &card, budget)
	if got := pitch(out, speaker.Rate); math.Abs(got-440) > 10 {
		t.Errorf("the answer played at %.0f Hz, want 440", got)
	}

	// The end of the answer is a speech on the control socket, which is how anything watching learns
	// that the device has stopped talking.
	if r := waitSpeech(t, c.term); r.Speech == nil || r.Speech.Packets != packets {
		t.Errorf("the control socket reported %+v, want a speech of %d packets", r.Speech, packets)
	}
	if c.f.Status().Speaking {
		t.Error("the status still says the device is speaking after the answer was heard")
	}
}

func (c *client) downlink(t *testing.T) *downlink {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if d := c.f.downlink(); d != nil {
			return d
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("the session never built a downlink")
	return nil
}

// waitDecoded waits for the downlink to have placed n frames. The read loop is the socket's own
// goroutine, so a test that looked right after the cloud wrote would be looking at the write.
func waitDecoded(t *testing.T, d *downlink, frames int) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		d.mu.Lock()
		got, pkts := d.frames, d.packets
		d.mu.Unlock()
		if got >= frames {
			return
		}
		if pkts >= frames/2880+2 {
			// Every packet accounted for and the frames still short: a decode that produced the
			// wrong number of samples, which is the chipmunk arriving by another route.
			t.Fatalf("the downlink took %d packets but placed only %d of %d frames", pkts, got, frames)
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("the downlink never decoded the answer")
}

// drive renders until the downlink gives the speaker back, and returns what was played.
func drive(t *testing.T, d *downlink, card *atomic.Uint64, cap int) []int16 {
	t.Helper()
	blk := make([]int16, speakerPeriod*speaker.Channels)
	out := make([]int16, 0, cap*speaker.Channels)
	for i := 0; i < cap/speakerPeriod && d.isSpeaking(); i++ {
		d.Render(card.Load(), blk)
		card.Add(speakerPeriod)
		out = append(out, blk...)
	}
	if d.isSpeaking() {
		t.Fatal("the downlink never gave the speaker back")
	}
	return out
}

// waitSpeech reads control lines until the speech event arrives.
func waitSpeech(t *testing.T, term *terminal) reply {
	t.Helper()
	for i := 0; i < 20; i++ {
		r, err := term.next()
		if err != nil {
			t.Fatalf("reading the control socket: %v", err)
		}
		if r.Event == eventSpeech {
			return r
		}
	}
	t.Fatal("no speech event reached the control socket")
	return reply{}
}

// The action button is the plan's "the button stops it mid-sentence", and the part that is easy to
// get wrong is not the stopping: it is the button's *other* meaning. A press means "make it stop"
// while something is being said and "ask me something" while nothing is, so a barge that did not
// report whether it found anything would leave the caller unable to tell those apart — and the
// caller is the action button, whose fallback is to open a turn.
//
// So both halves are tested, and the false case is the one that would be missed by a test that only
// ever stopped something.
func TestTheActionButtonStopsAnAnswerAndSaysThatItDid(t *testing.T) {
	c := connect(t)

	if c.f.Barge() {
		t.Fatal("a barge with nothing playing claimed to have stopped something: the action " +
			"button would take that as permission to open a turn")
	}

	c.cloud.speak(t, answer(t, defaultDownlinkRate, 60, 25, 440))
	down := c.downlink(t)
	waitDecoded(t, down, 25*speaker.Rate*60/1000)

	if !c.f.Barge() {
		t.Fatal("an answer was playing and the barge did not claim to have stopped it")
	}
	if down.isSpeaking() {
		t.Error("the answer is still speaking after the button stopped it")
	}
	if c.f.Barge() {
		t.Error("a second press of the button found the same answer still running")
	}
}
