package home

import (
	"context"
	"errors"
	"io"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/HuskerMinion/techo5/echod/internal/feature/media"
)

// A view that ends while one way to hear its camera is tried starts no other: not the next stream, and
// not Home Assistant's call.
func TestNoMoreWaysAreTriedForAViewThatEnded(t *testing.T) {
	fake := fakeOverFor(t)
	fake.player.Answer = func(media.OverToken, func(context.Context) (io.ReadCloser, error)) bool { return true }
	env := fake.env()
	calls := 0
	env.call = func(string, string) error { calls++; return nil }
	f := &Feature{}
	t.Cleanup(func() { endView(f) })
	type opener = func(context.Context) (io.ReadCloser, error)
	second := 0
	prev := soundSources
	soundSources = func(string) []opener {
		return []opener{
			func(context.Context) (io.ReadCloser, error) {
				f.mu.Lock()
				f.cam.Until = time.Time{} // the view ends while its first way is tried
				f.camSound, f.camOver = "", 0
				f.mu.Unlock()
				return nil, errors.New("refused")
			},
			func(context.Context) (io.ReadCloser, error) {
				second++
				return io.NopCloser(strings.NewReader("pcm")), nil
			},
		}
	}
	t.Cleanup(func() { soundSources = prev })

	f.askCameraSound("camera.porch", viewWithSound(f, "camera.porch", env), env)
	if second != 0 || calls != 0 {
		t.Errorf("after the view ended: the next way opened %d times, Home Assistant asked %d times", second, calls)
	}
}

// blockedSound is a decoder that has brought nothing yet: a read waits until its context ends, as one
// does when the decoder is killed.
type blockedSound struct{ ctx context.Context }

func (b blockedSound) Read([]byte) (int, error) { <-b.ctx.Done(); return 0, io.ErrUnexpectedEOF }
func (b blockedSound) Close() error             { return nil }

// A sound still starting for a view that has ended is ended at once, not when its wait for the first of
// it runs out.
func TestAStartingSoundEndsWithItsView(t *testing.T) {
	var gone atomic.Bool
	go func() { time.Sleep(30 * time.Millisecond); gone.Store(true) }()
	began := time.Now()
	src, _ := startSound(func(ctx context.Context) (io.ReadCloser, error) { return blockedSound{ctx}, nil },
		func() bool { return !gone.Load() }, time.Millisecond)
	if src != nil {
		t.Fatal("a sound that brought nothing was started")
	}
	if took := time.Since(began); took > 5*time.Second {
		t.Errorf("the sound of a view that ended went on starting for %s", took.Round(time.Millisecond))
	}
}

// A sound made here is closed, its decoder with it, when the player's context for it ends: silenced, or
// given up on for bringing nothing, while a read of it waits.
func TestASoundMadeHereClosesWithThePlayersContext(t *testing.T) {
	fake := fakeOverFor(t)
	src := &closeCounter{Reader: strings.NewReader("pcm")}
	played := make(chan struct{})
	fake.player.Answer = func(_ media.OverToken, open func(context.Context) (io.ReadCloser, error)) bool {
		ctx, cancel := context.WithCancel(context.Background())
		if _, err := open(ctx); err != nil {
			t.Error(err)
		}
		cancel()
		close(played)
		return true
	}
	env := fake.env()
	env.call = func(string, string) error { return nil }
	prev := soundSources
	soundSources = func(string) []func(context.Context) (io.ReadCloser, error) {
		return []func(context.Context) (io.ReadCloser, error){func(context.Context) (io.ReadCloser, error) { return src, nil }}
	}
	t.Cleanup(func() { soundSources = prev })
	f := &Feature{}
	t.Cleanup(func() { endView(f) })
	f.askCameraSound("camera.porch", viewWithSound(f, "camera.porch", env), env)
	<-played
	deadline := time.Now().Add(2 * time.Second)
	for src.closes.Load() == 0 {
		if time.Now().After(deadline) {
			t.Fatal("the sound was not closed when the player's context for it ended")
		}
		time.Sleep(time.Millisecond)
	}
}
