package streaming

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"sync"
	"time"

	"github.com/HuskerMinion/techo5/echod/internal/feature/media"
)

// What the receivers send, as they are told to send it: 16-bit stereo at 44.1 kHz, interleaved.
const (
	audioRate     = 44100
	audioChannels = 2
)

// setAside is how long a receiver must be quiet, after something else took the speaker from it, before
// what it sends next counts as asked for again: an app still playing to a speaker that was given
// something else does not take it straight back, as on any speaker; pausing and playing again does.
var setAside = 2 * time.Second

// play is what plays a receiver's audio; media.Get().PlayReceived, a variable for the tests.
var play = func(name string, src media.PCMSource, rate, channels int) {
	media.Get().PlayReceived(name, src, rate, channels)
}

// pump plays what a receiver writes to r as the track named name: a track starts whenever audio arrives
// and none of the receiver's own is playing, and runs until the receiver goes quiet or something else
// takes the speaker. r stays open across tracks and across the program's restarts.
func pump(ctx context.Context, name string, r *os.File) {
	buf := make([]byte, 16384)
	for ctx.Err() == nil {
		_ = r.SetReadDeadline(time.Time{})
		n, err := r.Read(buf)
		if err != nil {
			if ctx.Err() == nil {
				slog.Warn("streaming: reading a receiver's audio failed", "from", name, "err", err)
			}
			return
		}
		if n == 0 {
			continue
		}
		src := &pipeSource{f: r, pending: append([]byte(nil), buf[:n]...), done: make(chan struct{})}
		slog.Info("streaming: audio arrived", "from", name)
		play(name, src, audioRate, audioChannels)
		select {
		case <-ctx.Done():
			src.Close()
			return
		case <-src.done:
		}
		// A track that ended because the receiver went quiet is simply over: what it sends next is a new
		// one. One that was taken from it (something else played, or it was paused here, which no phone
		// hears of) leaves the receiver still sending: that is set aside until it has been quiet a
		// moment, which a pause on the phone gives.
		if !src.wentQuiet() {
			drain(ctx, r, buf)
		}
	}
}

// drain reads and drops what r sends until it has sent nothing for setAside. It reads no faster than
// the audio would play: librespot writes as fast as it is read, and drained flat out it would race
// through the listener's queue while the speaker plays something else.
func drain(ctx context.Context, r *os.File, buf []byte) {
	const bytesPerSecond = audioRate * audioChannels * 2
	start, read := time.Now(), int64(0)
	for ctx.Err() == nil {
		switch ahead := time.Duration(read)*time.Second/bytesPerSecond - time.Since(start); {
		case ahead > 0:
			select {
			case <-ctx.Done():
				return
			case <-time.After(ahead):
			}
		case ahead < -time.Second:
			// Behind (the sender stalled): paced from now, rather than read flat out to catch up.
			start, read = time.Now(), 0
		}
		_ = r.SetReadDeadline(time.Now().Add(setAside))
		n, err := r.Read(buf)
		read += int64(n)
		if err != nil {
			if !errors.Is(err, os.ErrDeadlineExceeded) && ctx.Err() == nil {
				slog.Warn("streaming: reading a receiver's audio failed", "err", err)
			}
			return
		}
	}
}

// pipeSource is one track's view of a receiver's pipe: what was read before it started, then the pipe
// itself. Closing it ends the track's reads without closing the pipe, which the next track reads on.
type pipeSource struct {
	f       *os.File
	mu      sync.Mutex
	pending []byte
	closed  bool
	quiet   bool // the track's last read found the receiver quiet
	once    sync.Once
	done    chan struct{}
}

func (s *pipeSource) Read(p []byte) (int, error) {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return 0, os.ErrClosed
	}
	if len(s.pending) > 0 {
		n := copy(p, s.pending)
		s.pending = s.pending[n:]
		s.mu.Unlock()
		return n, nil
	}
	s.mu.Unlock()
	n, err := s.f.Read(p)
	s.mu.Lock()
	defer s.mu.Unlock()
	switch {
	case s.closed:
		// Over while the read waited: what it took belongs to whatever reads the pipe next, and is
		// dropped rather than queued to a track that has ended.
		return 0, os.ErrClosed
	case errors.Is(err, os.ErrDeadlineExceeded):
		s.quiet = true
	}
	return n, err
}

// wentQuiet is whether the track ended on the receiver going quiet, not by being closed.
func (s *pipeSource) wentQuiet() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.quiet
}

func (s *pipeSource) SetReadDeadline(t time.Time) error { return s.f.SetReadDeadline(t) }

func (s *pipeSource) Close() error {
	s.once.Do(func() {
		s.mu.Lock()
		s.closed = true
		s.mu.Unlock()
		// A read waiting on the pipe is let go now, rather than whenever the receiver next writes.
		_ = s.f.SetReadDeadline(time.Now())
		close(s.done)
	})
	return nil
}
