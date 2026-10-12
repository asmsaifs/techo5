//go:build !dot

package video

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// The decoder is searched a piece at a time, and a needle that falls across two pieces is still found.
func TestFileHasFindsWhatFallsAcrossPieces(t *testing.T) {
	saved := fileChunk
	t.Cleanup(func() { fileChunk = saved })
	fileChunk = 7
	path := filepath.Join(t.TempDir(), "ffmpeg")
	body := strings.Repeat("x", 5) + "rtsp_transport" + strings.Repeat("y", 11) + "needle-two" + "zz"
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, chunk := range []int{1, 3, 7, 13, 64} {
		fileChunk = chunk
		if ok, err := fileHas(path, []byte("rtsp_transport"), []byte("needle-two")); err != nil || !ok {
			t.Errorf("chunk %d: both needles are in the file, fileHas said %v, %v", chunk, ok, err)
		}
		if ok, _ := fileHas(path, []byte("rtsp_transport"), []byte("not-there")); ok {
			t.Errorf("chunk %d: a needle that is not in the file was found", chunk)
		}
	}
	if _, err := fileHas(filepath.Join(t.TempDir(), "missing")); err == nil {
		t.Error("a missing file was read")
	}
}

// RTSP is used only with a decoder that has the demuxer and ignores a camera's Basic challenge: one
// without the change would send the login in the clear to a camera that offers only Basic.
func TestRTSPWantsADecoderThatRefusesBasic(t *testing.T) {
	dir := t.TempDir()
	write := func(name, body string) string {
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, []byte(body), 0o755); err != nil {
			t.Fatal(err)
		}
		return p
	}
	old := write("old", "....rtsp_transport....")
	patched := write("patched", "....rtsp_transport...."+string(digestOnly)+"; only Digest is answered\n")
	none := write("none", "....hls....")
	if readsRTSP(old) {
		t.Error("a decoder that would answer Basic is used for RTSP")
	}
	if !readsRTSP(patched) {
		t.Error("the image's decoder, built to ignore Basic, is not used for RTSP")
	}
	if readsRTSP(none) || readsRTSP(filepath.Join(dir, "missing")) {
		t.Error("a decoder without the RTSP demuxer is used for RTSP")
	}
	// What build-ffmpeg.sh patches in is what is looked for.
	script, err := os.ReadFile("../../../../tools/linux/build-ffmpeg.sh")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(script), string(digestOnly)) {
		t.Error("build-ffmpeg.sh does not patch in the message CanRTSP looks for")
	}
}

// A camera's frames are read into one picture for the whole stream, not a new one each.
func TestACameraReadsIntoOnePicture(t *testing.T) {
	fake(t, 3, true)
	cam, err := OpenCamera(context.Background(), "http://192.0.2.10/x.m3u8", testW, testH)
	if err != nil {
		t.Fatal(err)
	}
	defer cam.Close()
	a, err := cam.Next(5 * time.Second)
	if err != nil {
		t.Fatal(err)
	}
	b, err := cam.Next(5 * time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if a != b {
		t.Error("each frame came in a picture of its own")
	}
	if a.Bounds().Dx() != testW || a.Bounds().Dy() != testH {
		t.Errorf("a frame of %v, want %dx%d", a.Bounds(), testW, testH)
	}
}

// Turning Live camera video off with a camera's stream open through the proxy leaves the proxy up
// under it, and closes it once that stream has ended.
func TestTheGuardOutlastsAnOpenCameraStream(t *testing.T) {
	fake(t, 1, true)
	saved := guardAllowed
	t.Cleanup(func() { guardAllowed = saved; closeGuard() })
	guardAllowed = func() bool { return true }
	if _, err := guardProxy(); err != nil {
		t.Fatal(err)
	}
	cam, err := OpenCamera(context.Background(), "http://192.0.2.10/x.m3u8", testW, testH)
	if err != nil {
		t.Fatal(err)
	}
	guardAllowed = func() bool { return false }
	CameraLiveChanged()
	guardMu.Lock()
	up := guardURL != ""
	guardMu.Unlock()
	if !up {
		t.Fatal("the proxy was closed under an open camera stream")
	}
	cam.Close()
	deadline := time.Now().Add(5 * time.Second)
	for {
		guardMu.Lock()
		up, runs := guardURL != "", cameraRuns
		guardMu.Unlock()
		if !up && runs == 0 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("the proxy is still up (runs %d) after the last camera stream ended with the switch off", runs)
		}
		time.Sleep(10 * time.Millisecond)
	}
}
