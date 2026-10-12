//go:build !dot

package home

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"github.com/HuskerMinion/techo5/echod/internal/config"
	"github.com/HuskerMinion/techo5/echod/internal/feature/video"
)

// The test binary stands in for the decoder in these tests: run with TECHO5_HOME_FAKE_DECODER naming a
// directory, it leaves its process number there, then fails at once on an RTSP address (a camera that
// brings no picture) and on anything else waits to be killed (a stream whose first frame is slow).
func TestMain(m *testing.M) {
	if dir := os.Getenv("TECHO5_HOME_FAKE_DECODER"); dir != "" {
		_ = os.WriteFile(filepath.Join(dir, strconv.Itoa(os.Getpid())), nil, 0o644)
		for _, a := range os.Args[1:] {
			if strings.HasPrefix(a, "rtsp://") {
				os.Stderr.WriteString("no picture\n")
				os.Exit(1)
			}
		}
		select {}
	}
	os.Exit(m.Run())
}

// fakeDecoder makes the test binary the decoder for a live camera view, and returns where its runs
// leave their process numbers.
func fakeDecoder(t *testing.T) string {
	t.Helper()
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	pids := t.TempDir()
	t.Cleanup(video.UseDecoderForTests(exe, []string{"PATH=/usr/bin:/bin", "TECHO5_HOME_FAKE_DECODER=" + pids}))
	config.Use(filepath.Join(t.TempDir(), "state.json"))
	if err := config.Set().Home().CameraLive(true); err != nil {
		t.Fatal(err)
	}
	savedWait, savedCan, savedURL := shapeWait, canStream, cameraStreamURL
	t.Cleanup(func() { shapeWait, canStream, cameraStreamURL = savedWait, savedCan, savedURL })
	canStream = func(string) bool { return true }
	streamAsks.Store(0)
	cameraStreamURL = func(context.Context, string) (string, error) {
		streamAsks.Add(1)
		return "http://192.0.2.10/hls/x.m3u8", nil
	}
	return pids
}

// streamAsks counts the asks for Home Assistant's stream, each the start of its decoder.
var streamAsks atomic.Int32

// decoderRuns are the process numbers the stand-in left.
func decoderRuns(t *testing.T, dir string) []int {
	t.Helper()
	ents, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	var out []int
	for _, e := range ents {
		if pid, err := strconv.Atoi(e.Name()); err == nil {
			out = append(out, pid)
		}
	}
	return out
}

// noDecoderLeft waits for every run of the stand-in to be gone.
func noDecoderLeft(t *testing.T, dir string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for _, pid := range decoderRuns(t, dir) {
		for !errors.Is(syscall.Kill(pid, 0), syscall.ESRCH) {
			if time.Now().After(deadline) {
				t.Fatalf("decoder %d is still running after its view ended", pid)
			}
			time.Sleep(10 * time.Millisecond)
		}
	}
}

// A view that runs out while its stream waits for a snapshot's shape starts no decoder at all.
func TestAViewThatEndsDuringTheShapeWaitStartsNoDecoder(t *testing.T) {
	pids := fakeDecoder(t)
	shapeWait = 300 * time.Millisecond
	f := newFeature(t)
	f.ShowCamera("camera.shape_wait", time.Minute)
	time.Sleep(50 * time.Millisecond)
	f.mu.Lock()
	f.cam.Until = time.Now() // runs out: nobody cancels anything
	f.mu.Unlock()
	time.Sleep(shapeWait + 500*time.Millisecond)
	if n, runs := streamAsks.Load(), decoderRuns(t, pids); n != 0 || len(runs) != 0 {
		t.Errorf("a stream was asked for %d times and %d decoders started for a view that had ended", n, len(runs))
	}
	noDecoderLeft(t, pids)
}

// A view taken down while its camera falls back from its RTSP address to Home Assistant's stream leaves
// no decoder behind: not the one that brought no picture, nor the stream's.
func TestNoDecoderOutlivesAViewThatFellBackToHLS(t *testing.T) {
	pids := fakeDecoder(t)
	shapeWait = 10 * time.Millisecond
	entity := "camera.fell_back"
	if err := config.Set().Security().TalkBack(true); err != nil {
		t.Fatal(err)
	}
	if err := config.Set().TalkBack().SaveWithPictures("viewer", nil, nil, map[string]string{entity: "rtsp://192.0.2.10/sub"}); err != nil {
		t.Fatal(err)
	}
	if !video.CanRTSP() {
		t.Skip("the stand-in decoder is not taken for one that reads RTSP")
	}
	f := newFeature(t)
	f.ShowCamera(entity, time.Minute)
	deadline := time.Now().Add(5 * time.Second)
	for len(decoderRuns(t, pids)) < 2 {
		if time.Now().After(deadline) {
			t.Fatalf("%d decoders started, want the RTSP one and then the stream's", len(decoderRuns(t, pids)))
		}
		time.Sleep(10 * time.Millisecond)
	}
	v, _ := f.Camera()
	if !f.HideView(v.View) {
		t.Fatal("the view was not up to take down")
	}
	noDecoderLeft(t, pids)
}

// A view that simply runs out while its stream waits for a first frame ends its decoder within a poll,
// not when the wait (streamStart) does.
func TestADecoderEndsWithAViewThatRunsOut(t *testing.T) {
	pids := fakeDecoder(t)
	shapeWait = 10 * time.Millisecond
	f := newFeature(t)
	f.ShowCamera("camera.runs_out", time.Minute)
	deadline := time.Now().Add(5 * time.Second)
	for len(decoderRuns(t, pids)) < 1 {
		if time.Now().After(deadline) {
			t.Fatal("no decoder started for the stream")
		}
		time.Sleep(10 * time.Millisecond)
	}
	f.mu.Lock()
	f.cam.Until = time.Now()
	f.mu.Unlock()
	noDecoderLeft(t, pids)
}
