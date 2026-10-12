package home

import (
	"context"
	"errors"
	"image"
	"io"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/HuskerMinion/techo5/echod/internal/feature/media"
)

// A camera with a stream is heard from it: its ask is answered here, and Home Assistant is not asked to
// send the stream back converted, which came a dozen seconds and more behind the picture. One without a
// stream, and one whose stream brings no sound (none in it, or one that cannot be read), is asked of Home
// Assistant as before, rather than left silent.
func TestACameraWithAStreamIsHeardFromIt(t *testing.T) {
	fake := fakeOverFor(t)
	var answered []media.OverToken
	fake.player.Answer = func(tk media.OverToken, open func(context.Context) (io.ReadCloser, error)) bool {
		answered = append(answered, tk)
		return true
	}
	env := fake.env()
	calls := 0
	env.call = func(string, string) error { calls++; return nil }
	type opener = func(context.Context) (io.ReadCloser, error)
	sound := func(context.Context) (io.ReadCloser, error) { return io.NopCloser(strings.NewReader("pcm")), nil }
	silent := func(context.Context) (io.ReadCloser, error) { return io.NopCloser(strings.NewReader("")), nil } // ffmpeg ends at once
	refused := func(context.Context) (io.ReadCloser, error) { return nil, errors.New("refused") }
	prev := soundSources
	soundSources = func(entity string) []opener {
		switch entity {
		case "camera.porch":
			return []opener{sound}
		case "camera.door": // its RTSP address refuses the login; its HLS stream has sound
			return []opener{refused, sound}
		case "camera.yard": // a stream with no sound in it
			return []opener{silent}
		case "camera.gate": // neither way can be opened
			return []opener{refused, refused}
		}
		return nil
	}
	t.Cleanup(func() { soundSources = prev })

	f := &Feature{}
	t.Cleanup(func() { endView(f) })
	for _, entity := range []string{"camera.porch", "camera.door"} {
		f.askCameraSound(entity, viewWithSound(f, entity, env), env)
		tk := soundToken(f)
		if answered[len(answered)-1] != tk || calls != 0 {
			t.Errorf("%s: answered %v (the view's ask is %d), Home Assistant asked %d times", entity, answered, tk, calls)
		}
		if !slices.Contains(fake.settledTokens, tk) {
			t.Errorf("%s: the ask was never settled", entity)
		}
	}

	for i, entity := range []string{"camera.garage", "camera.yard", "camera.gate"} {
		f.askCameraSound(entity, viewWithSound(f, entity, env), env)
		if len(answered) != 2 || calls != i+1 {
			t.Errorf("%s: answered %v, Home Assistant asked %d times", entity, answered, calls)
		}
	}
}

// viewWithSound puts entity's view up with an ask out for its sound, as startCameraSound does, and
// returns the ask.
func viewWithSound(f *Feature, entity string, env soundEnv) media.OverToken {
	tk := env.player.Ask()
	f.mu.Lock()
	f.cam = CameraView{Entity: entity, Until: time.Now().Add(time.Minute)}
	f.camSound, f.camOver = entity, tk
	f.mu.Unlock()
	return tk
}

// soundToken is the ask the view's sound is under now.
func soundToken(f *Feature) media.OverToken {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.camOver
}

// endView takes the view down and waits for what it started to notice.
func endView(f *Feature) {
	f.mu.Lock()
	f.cam.Until = time.Time{}
	f.mu.Unlock()
	f.loops.Wait()
}

// closeCounter is a sound whose closes are counted.
type closeCounter struct {
	io.Reader
	closes atomic.Int32
}

func (c *closeCounter) Close() error { c.closes.Add(1); return nil }

// A stream that takes as long to start as an ask waits does not leave Home Assistant's call, or its own
// sound, under an ask that ran out: either is played under a fresh one, so Home Assistant's url is not let
// through as a track. One whose view ended while it started is closed, and nothing else is asked.
func TestASlowStreamIsPlayedUnderAFreshAsk(t *testing.T) {
	fake := fakeOverFor(t)
	var answered []media.OverToken
	fake.player.Answer = func(tk media.OverToken, open func(context.Context) (io.ReadCloser, error)) bool {
		answered = append(answered, tk)
		return true
	}
	env := fake.env()
	calls := 0
	env.call = func(string, string) error { calls++; return nil }
	f := &Feature{}
	t.Cleanup(func() { endView(f) })

	type opener = func(context.Context) (io.ReadCloser, error)
	var sound *closeCounter
	prev := soundSources
	soundSources = func(entity string) []opener {
		return []opener{func(context.Context) (io.ReadCloser, error) {
			if entity == "camera.gone" {
				endView(f)                                   // the view is tapped away while the stream starts...
				f.cameraSoundOff(entity, soundToken(f), env) // ...and its watcher gives the sound up
			}
			if entity == "camera.silent" {
				return io.NopCloser(strings.NewReader("")), nil
			}
			sound = &closeCounter{Reader: strings.NewReader("pcm")}
			return sound, nil
		}}
	}
	t.Cleanup(func() { soundSources = prev })

	first := viewWithSound(f, "camera.porch", env)
	f.askCameraSound("camera.porch", first, env)
	if len(answered) != 1 || answered[0] == first || answered[0] != soundToken(f) {
		t.Errorf("answered %v under the first ask %d; the view's is %d", answered, first, soundToken(f))
	}
	fake.mu.Lock()
	gaveUp := slices.Contains(fake.dropped, first)
	fake.mu.Unlock()
	if !gaveUp {
		t.Error("the ask that waited was not given up")
	}

	first = viewWithSound(f, "camera.silent", env)
	f.askCameraSound("camera.silent", first, env)
	if calls != 1 || soundToken(f) == first {
		t.Errorf("Home Assistant asked %d times, under the ask %d that waited for the stream", calls, soundToken(f))
	}

	f.askCameraSound("camera.gone", viewWithSound(f, "camera.gone", env), env)
	if len(answered) != 1 || calls != 1 {
		t.Errorf("a view gone while its stream started: answered %v, Home Assistant asked %d times", answered, calls)
	}
	if sound.closes.Load() == 0 {
		t.Error("the stream of a view gone was left running")
	}
}

// A sound made here whose ask was given up on just as it was answered is never opened by the player, so
// it is closed here, which is what ends its decoder.
func TestASoundNeverPlayedIsClosed(t *testing.T) {
	fake := fakeOverFor(t)
	fake.player.Answer = func(tk media.OverToken, open func(context.Context) (io.ReadCloser, error)) bool {
		fake.stop(tk) // given up on: the real player drops it without opening it
		return true
	}
	env := fake.env()
	env.call = func(string, string) error { return nil }
	sound := &closeCounter{Reader: strings.NewReader("pcm")}
	type opener = func(context.Context) (io.ReadCloser, error)
	prev := soundSources
	soundSources = func(string) []opener {
		return []opener{func(context.Context) (io.ReadCloser, error) { return sound, nil }}
	}
	t.Cleanup(func() { soundSources = prev })

	f := &Feature{}
	f.askCameraSound("camera.porch", viewWithSound(f, "camera.porch", env), env)
	deadline := time.Now().Add(5 * time.Second)
	for sound.closes.Load() == 0 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if sound.closes.Load() == 0 {
		t.Error("a sound the player never opened was left running")
	}
	endView(f)
}

// A frame fetched for a view that has since been closed and opened again does not show in the new one:
// two views of the same camera are told apart by their number, not by the camera.
func TestAFrameShowsOnlyInTheViewItWasFetchedFor(t *testing.T) {
	f := &Feature{}
	f.cam = CameraView{Entity: "camera.porch", Until: time.Now().Add(time.Minute), gen: 2}
	f.showFrame("camera.porch", 1, image.NewRGBA(image.Rect(0, 0, 4, 4)), nil)
	if f.cam.Frame != nil {
		t.Error("a frame of the view before showed in this one")
	}
	if f.viewUp(context.Background(), "camera.porch", 1) {
		t.Error("the view before still counts as up")
	}
	f.showFrame("camera.porch", 2, image.NewRGBA(image.Rect(0, 0, 4, 4)), nil)
	if f.cam.Frame == nil {
		t.Error("this view's own frame did not show")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if f.viewUp(ctx, "camera.porch", 2) {
		t.Error("a view whose fetching was called off still counts as up")
	}
}
