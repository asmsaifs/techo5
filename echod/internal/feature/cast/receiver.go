// Package cast receives a phone's screen and audio, straight from the phone: no server in between.
// The phone finds the device, connects, proves it knows the pairing key, and sends JPEG frames and
// PCM audio stamped with its own clock; the device works out the difference between the two clocks
// and shows each frame when its time comes, dropping the ones that are late.
//
// This package is the protocol and the timing. What to do with a frame or a sample, and whether to
// take the cast at all, is the Sink's.
package cast

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"image"
	"image/jpeg"
	"log/slog"
	"net"
	"strings"
	"sync"
	"time"
	"unicode"
)

// Sink is what a cast plays on: the screen and the speaker, and the say on whether to start.
type Sink interface {
	// Begin is asked when a phone has proved it has the key. It answers for the device: a refusal
	// (busy, in a call, the person said no) or the screen's size and how long to buffer. It runs
	// before anything is shown or played.
	Begin(h Hello) (Welcome, error)
	// Frame shows a frame, at the moment it is due. img is the Sink's to keep.
	Frame(img image.Image)
	// Audio takes interleaved S16LE stereo PCM that should be heard at at, on the device's clock.
	// pcm is the Sink's to keep.
	Audio(pcm []byte, at time.Time)
	// End is called once when the cast is over, however it ended.
	End()
}

// Receiver holds a port for phones to connect to, one at a time.
type Receiver struct {
	// Key returns the pairing key; read at each connection so a changed setting applies at once.
	Key  func() string
	Sink Sink

	mu   sync.Mutex
	busy bool
}

// lateMax is how far past its time a frame is still shown. Later than that, showing it only makes the
// next one later still.
const lateMax = 100 * time.Millisecond

// idle is how long a phone may say nothing, not even its clock, before the cast is ended.
const idle = 10 * time.Second

// farMax is how far ahead of now a stamp may be: the latency is a fraction of a second, so anything
// past this is a phone with a wrong clock, or one making trouble.
const farMax = 2 * time.Second

// queued is how many frames may wait to be decoded and shown; when it is full the oldest goes.
const queued = 8

// Serve accepts phones on ln until ctx ends.
func (r *Receiver) Serve(ctx context.Context, ln net.Listener) error {
	go func() {
		<-ctx.Done()
		_ = ln.Close()
	}()
	for {
		c, err := ln.Accept()
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}
			return err
		}
		if !r.take() {
			c.Close() // one cast at a time, and no handshake spent on the second
			continue
		}
		go func() {
			defer r.give()
			if err := r.session(ctx, c); err != nil && ctx.Err() == nil {
				slog.Info("cast ended", "from", c.RemoteAddr(), "err", err)
			}
		}()
	}
}

func (r *Receiver) take() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.busy {
		return false
	}
	r.busy = true
	return true
}

func (r *Receiver) give() {
	r.mu.Lock()
	r.busy = false
	r.mu.Unlock()
}

func (r *Receiver) session(ctx context.Context, raw net.Conn) error {
	defer raw.Close()
	stop := context.AfterFunc(ctx, func() { raw.Close() })
	defer stop()

	_ = raw.SetDeadline(time.Now().Add(10 * time.Second))
	key := r.Key()
	if len(key) < 4 {
		return errors.New("no pairing key set")
	}
	c, err := accept(raw, key)
	if err != nil {
		return err
	}
	kind, payload, err := Read(c)
	if err != nil {
		return err
	}
	if kind != KindHello {
		return errors.New("the first message was not a hello")
	}
	var h Hello
	if err := json.Unmarshal(payload, &h); err != nil {
		return errors.New("a hello that is not JSON")
	}
	h.Name = clean(h.Name)

	w, err := r.Sink.Begin(h)
	if err != nil {
		w = Welcome{OK: false, Reason: err.Error()}
	}
	out, _ := json.Marshal(w)
	if err := Write(c, KindWelcome, out); err != nil {
		return err
	}
	if !w.OK {
		return errors.New("refused: " + w.Reason)
	}
	defer r.Sink.End()
	slog.Info("cast started", "from", raw.RemoteAddr(), "phone", h.Name)

	s := &play{
		sink: r.Sink, w: w, h: h, start: time.Now(),
		latency: time.Duration(w.LatencyMs) * time.Millisecond,
		frames:  make(chan frame, queued),
	}
	var wg sync.WaitGroup
	wg.Add(1)
	go func() { defer wg.Done(); s.show() }()
	defer func() { close(s.frames); wg.Wait() }()

	for {
		_ = raw.SetReadDeadline(time.Now().Add(idle))
		kind, payload, err := Read(c)
		if err != nil {
			return err
		}
		switch kind {
		case KindClock:
			if us, _, ok := Split(payload); ok {
				s.observe(us)
			}
		case KindVideo:
			if h.Video {
				s.video(payload)
			}
		case KindAudio:
			if h.Audio {
				s.audio(payload)
			}
		case KindBye:
			return nil
		}
	}
}

// clean makes a phone's name safe to put on a screen: printable, and short.
func clean(name string) string {
	name = strings.Map(func(r rune) rune {
		if unicode.IsPrint(r) {
			return r
		}
		return -1
	}, name)
	if r := []rune(name); len(r) > 40 {
		name = string(r[:40])
	}
	if name == "" {
		name = "A phone"
	}
	return name
}

// clockWindow is how many clock messages the offset is taken over.
const clockWindow = 10

// play is one cast's timing: the offset between the phone's clock and this one, and the frames
// waiting for their moment.
type play struct {
	sink    Sink
	w       Welcome
	h       Hello
	start   time.Time
	latency time.Duration
	frames  chan frame

	mu      sync.Mutex
	samples [clockWindow]int64 // arrival minus the phone's stamp, µs; the smallest carries least delay
	n       int
	offset  int64
	known   bool
}

type frame struct {
	due time.Time
	jpg []byte
}

// now is this device's clock in µs since the cast began.
func (p *play) now() int64 { return time.Since(p.start).Microseconds() }

// observe takes a clock message. The network only ever adds delay, so of the last few the smallest
// difference is the truest.
func (p *play) observe(phoneUs int64) {
	d := p.now() - phoneUs
	p.mu.Lock()
	defer p.mu.Unlock()
	p.samples[p.n%clockWindow] = d
	p.n++
	lo := d
	for i := 0; i < min(p.n, clockWindow); i++ {
		lo = min(lo, p.samples[i])
	}
	p.offset, p.known = lo, true
}

// due is when a stamp is to be presented, on this device's clock; false before the first clock
// message, when nothing can be placed.
func (p *play) due(phoneUs int64) (time.Time, bool) {
	p.mu.Lock()
	off, ok := p.offset, p.known
	p.mu.Unlock()
	if !ok {
		return time.Time{}, false
	}
	return p.start.Add(time.Duration(phoneUs+off) * time.Microsecond).Add(p.latency), true
}

func (p *play) video(payload []byte) {
	us, jpg, ok := Split(payload)
	if !ok {
		return
	}
	due, ok := p.due(us)
	if !ok || time.Since(due) > lateMax {
		return
	}
	f := frame{due: due, jpg: jpg}
	for {
		select {
		case p.frames <- f:
			return
		default:
			select { // full: the oldest is the one least worth showing
			case <-p.frames:
			default:
			}
		}
	}
}

func (p *play) audio(payload []byte) {
	us, pcm, ok := Split(payload)
	if !ok || len(pcm) == 0 || len(pcm)%4 != 0 {
		return
	}
	due, ok := p.due(us)
	if !ok {
		return
	}
	if time.Until(due) > farMax {
		return
	}
	p.sink.Audio(pcm, due)
}

// show decodes frames and puts each up when its time comes. A frame whose time has gone by when the
// next is already waiting is skipped without being decoded: on a slow device that is what keeps the
// picture on the present.
func (p *play) show() {
	for f := range p.frames {
		if len(p.frames) > 0 && time.Since(f.due) > 0 {
			continue
		}
		img, err := p.decode(f.jpg)
		if err != nil {
			continue
		}
		if wait := time.Until(f.due); wait > 0 {
			if wait > farMax {
				continue // a stamp from nowhere near now: not something to sleep on
			}
			time.Sleep(wait)
		} else if -wait > lateMax {
			continue
		}
		p.sink.Frame(img)
	}
}

// decode reads a frame's size before decoding it, so a picture claiming to be enormous gets nothing
// decoded rather than all of memory.
func (p *play) decode(b []byte) (image.Image, error) {
	cfg, err := jpeg.DecodeConfig(bytes.NewReader(b))
	if err != nil {
		return nil, err
	}
	if cfg.Width <= 0 || cfg.Height <= 0 || cfg.Width > p.w.W || cfg.Height > p.w.H {
		return nil, errors.New("cast: a frame larger than the screen")
	}
	return jpeg.Decode(bytes.NewReader(b))
}
