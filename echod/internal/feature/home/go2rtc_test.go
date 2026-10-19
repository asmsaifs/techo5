package home

import (
	"bytes"
	"image"
	"image/jpeg"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/HuskerMinion/techo5/echod/internal/config"
)

// fakeGo2RTC is a go2rtc with two streams, a picture of each, and the requests it was sent.
func fakeGo2RTC(t *testing.T) (*httptest.Server, *[]string) {
	t.Helper()
	var pic bytes.Buffer
	if err := jpeg.Encode(&pic, image.NewRGBA(image.Rect(0, 0, 640, 360)), nil); err != nil {
		t.Fatal(err)
	}
	var asked []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		asked = append(asked, r.URL.RequestURI())
		switch r.URL.Path {
		case "/api/streams":
			_, _ = w.Write([]byte(`{"porch":{},"drive":{}}`))
		case "/api/frame.jpeg":
			_, _ = w.Write(pic.Bytes())
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	return srv, &asked
}

func TestGo2RTCStreamsBecomeCameras(t *testing.T) {
	config.Use(filepath.Join(t.TempDir(), "state.json"))
	srv, asked := fakeGo2RTC(t)
	n, err := SetGo2RTC(srv.URL)
	if err != nil || n != 2 {
		t.Fatalf("set: %d %v", n, err)
	}
	f := newFeature(t)
	var names []string
	for _, c := range f.Cameras() {
		if isGo2RTC(c.Entity) {
			names = append(names, c.Name+"="+c.Entity)
		}
	}
	if strings.Join(names, ",") != "drive=go2rtc:drive,porch=go2rtc:porch" {
		t.Errorf("cameras %v", names)
	}

	frame, err := f.snapshot("go2rtc:porch")
	if err != nil || frame == nil {
		t.Fatalf("snapshot: %v", err)
	}
	if !slices.ContainsFunc(*asked, func(u string) bool { return strings.HasPrefix(u, "/api/frame.jpeg?src=porch") }) {
		t.Errorf("go2rtc was asked %v", *asked)
	}
	if addr, ok := go2rtcRTSP("go2rtc:porch"); !ok || !strings.HasSuffix(addr, ":8554/porch") {
		t.Errorf("rtsp %q", addr)
	}

	if n, err := SetGo2RTC(""); err != nil || n != 0 || len(config.Get().Home.Go2RTC.Cameras) != 0 {
		t.Errorf("taken away: %d %v %+v", n, err, config.Get().Home.Go2RTC)
	}
}

func TestAGo2RTCThatDoesNotAnswerChangesNothing(t *testing.T) {
	config.Use(filepath.Join(t.TempDir(), "state.json"))
	srv, _ := fakeGo2RTC(t)
	if _, err := SetGo2RTC(srv.URL); err != nil {
		t.Fatal(err)
	}
	gone := httptest.NewServer(http.NotFoundHandler())
	gone.Close()
	if _, err := SetGo2RTC(gone.URL); err == nil {
		t.Error("a server that is not there was set up")
	}
	if len(config.Get().Home.Go2RTC.Cameras) != 2 {
		t.Error("a failed set took the cameras away")
	}
}
