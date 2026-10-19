package remote

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	esphome "github.com/ygelfand/go-esphome-device"

	"github.com/HuskerMinion/techo5/echod/internal/config"
)

// testRemote is the API with a key made and a few entities of its own.
func testRemote(t *testing.T) (*Remote, map[string]esphome.Entity, *int) {
	t.Helper()
	config.Use(filepath.Join(t.TempDir(), "state.json"))
	if err := config.Set().Remote().Secret(SecretOf(testKey)); err != nil {
		t.Fatal(err)
	}
	if err := config.Set().Remote().On(true); err != nil {
		t.Fatal(err)
	}
	pressed := 0
	vol := &esphome.Number{Base: esphome.Base{ObjectID: "ring_volume", Category: esphome.CategoryConfig}, Min: 0, Max: 10, Step: 1}
	vol.OnCommand = func(v float32) { vol.Set(v) }
	es := map[string]esphome.Entity{
		"lyrics":       &esphome.Switch{Base: esphome.Base{ObjectID: "lyrics", Category: esphome.CategoryConfig}},
		"ring_volume":  vol,
		"screen_theme": &esphome.Select{Base: esphome.Base{ObjectID: "screen_theme", Category: esphome.CategoryConfig}, Options: []string{"Ember", "Ocean"}},
		"alarm_stop":   &esphome.Button{Base: esphome.Base{ObjectID: "alarm_stop"}, OnPress: func() { pressed++ }},
		"ssh":          &esphome.Switch{Base: esphome.Base{ObjectID: "ssh", Category: esphome.CategoryConfig}},
		"min_cores":    &esphome.Number{Base: esphome.Base{ObjectID: "min_cores", Category: esphome.CategoryDiagnostic}, Max: 8},
		"lux":          &esphome.Sensor{Base: esphome.Base{ObjectID: "lux"}},
	}
	f := build()
	f.entities = func() []esphome.Entity {
		out := make([]esphome.Entity, 0, len(es))
		for _, e := range es {
			out = append(out, e)
		}
		return out
	}
	return f, es, &pressed
}

func call(f *Remote, method, path, body string, signed bool) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, bytes.NewReader([]byte(body)))
	req.RemoteAddr = "192.0.2.50:4000"
	if signed {
		req.Header.Set(authHeader, Sign(testKey, method, path, []byte(body), time.Now()))
	}
	w := httptest.NewRecorder()
	f.serve(w, req)
	return w
}

func TestTheAPINeedsASignature(t *testing.T) {
	f, _, _ := testRemote(t)
	if w := call(f, "GET", "/api/v1/device", "", false); w.Code != http.StatusUnauthorized {
		t.Errorf("unsigned: %d", w.Code)
	}
	if w := call(f, "GET", "/api/v1/device", "", true); w.Code != http.StatusOK {
		t.Errorf("signed: %d %s", w.Code, w.Body)
	}
}

func TestItIsClosedWithoutAKeyOrWhenOff(t *testing.T) {
	f, _, _ := testRemote(t)
	if !f.open() {
		t.Fatal("on with a key, it is not open")
	}
	_ = config.Set().Remote().On(false)
	if f.open() {
		t.Error("off, it is open")
	}
	_ = config.Set().Remote().On(true)
	_ = config.Set().Remote().Secret("")
	if f.open() {
		t.Error("with no key, it is open")
	}
}

func TestEntitiesAreListedAndSet(t *testing.T) {
	f, es, pressed := testRemote(t)
	w := call(f, "GET", "/api/v1/entities", "", true)
	var list []entity
	if err := json.Unmarshal(w.Body.Bytes(), &list); err != nil || len(list) != len(es) {
		t.Fatalf("listing: %d %v %s", len(list), err, w.Body)
	}
	byID := map[string]entity{}
	for _, e := range list {
		byID[e.ID] = e
	}
	if !byID["lyrics"].Writable || byID["ssh"].Writable || byID["min_cores"].Writable || byID["lux"].Writable {
		t.Errorf("writable: %+v", byID)
	}

	for _, c := range []struct {
		path, body string
		want       int
	}{
		{"/api/v1/entities/lyrics", `{"state":true}`, http.StatusNoContent},
		{"/api/v1/entities/ring_volume", `{"state":7}`, http.StatusNoContent},
		{"/api/v1/entities/ring_volume", `{"state":70}`, http.StatusBadRequest},
		{"/api/v1/entities/screen_theme", `{"state":"Ocean"}`, http.StatusNoContent},
		{"/api/v1/entities/screen_theme", `{"state":"Mauve"}`, http.StatusBadRequest},
		{"/api/v1/entities/alarm_stop", "", http.StatusNoContent},
		{"/api/v1/entities/ssh", `{"state":true}`, http.StatusForbidden},
		{"/api/v1/entities/min_cores", `{"state":2}`, http.StatusForbidden},
		{"/api/v1/entities/lux", `{"state":2}`, http.StatusForbidden},
		{"/api/v1/entities/nothing", `{"state":2}`, http.StatusNotFound},
		{"/api/v1/entities/lyrics", `{"state":"yes"}`, http.StatusBadRequest},
	} {
		if w := call(f, "POST", c.path, c.body, true); w.Code != c.want {
			t.Errorf("%s %s: %d, want %d (%s)", c.path, c.body, w.Code, c.want, w.Body)
		}
	}
	if !es["lyrics"].(*esphome.Switch).Get() || es["ring_volume"].(*esphome.Number).Get() != 7 ||
		es["screen_theme"].(*esphome.Select).Get() != "Ocean" || *pressed != 1 {
		t.Error("the changes did not land")
	}
	if es["ssh"].(*esphome.Switch).Get() {
		t.Error("SSH was turned on through the API")
	}
}

// The log comes redacted, the last of it first and then what is written after.
func TestTheLogIsFollowed(t *testing.T) {
	f, _, _ := testRemote(t)
	path := filepath.Join(t.TempDir(), "techo5.log")
	if err := os.WriteFile(path, []byte("old line\nmy secret line\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	was, wasRed := logPath, newRedactor
	logPath = path
	newRedactor = func() func(string) string {
		return func(s string) string { return strings.ReplaceAll(s, "secret", "<hidden>") }
	}
	t.Cleanup(func() { logPath, newRedactor = was, wasRed })

	srv := httptest.NewServer(http.HandlerFunc(f.serve))
	defer srv.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, "GET", srv.URL+"/api/v1/logs", nil)
	req.Header.Set(authHeader, Sign(testKey, "GET", "/api/v1/logs", nil, time.Now()))
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK || resp.Header.Get("Content-Type") != "text/event-stream" {
		t.Fatalf("%d %s", resp.StatusCode, resp.Header.Get("Content-Type"))
	}
	lines := bufio.NewScanner(resp.Body)
	next := func() string {
		for lines.Scan() {
			if s, ok := strings.CutPrefix(lines.Text(), "data: "); ok {
				return s
			}
		}
		return ""
	}
	if got := next(); got != "old line" {
		t.Errorf("first: %q", got)
	}
	if got := next(); got != "my <hidden> line" {
		t.Errorf("second, redacted: %q", got)
	}
	fh, _ := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0)
	_, _ = fh.WriteString("a new line\n")
	fh.Close()
	if got := next(); got != "a new line" {
		t.Errorf("followed: %q", got)
	}
}

// Only two can follow the log at once.
func TestOnlyTwoFollowTheLog(t *testing.T) {
	f, _, _ := testRemote(t)
	streams.Store(maxStreams)
	t.Cleanup(func() { streams.Store(0) })
	if w := call(f, "GET", "/api/v1/logs", "", true); w.Code != http.StatusTooManyRequests {
		t.Errorf("a third stream: %d", w.Code)
	}
}
