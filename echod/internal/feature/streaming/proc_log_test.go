package streaming

import (
	"bytes"
	"log/slog"
	"strings"
	"testing"
	"time"
)

// librespot's errors reach the log a user sends, at Warn, and a skip loop's same error is one line in
// half a minute, whatever its time stamp; a program with no filter stays at Debug.
func TestKeptLinesAreWarningsOnceInAWhile(t *testing.T) {
	var buf bytes.Buffer
	old := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelInfo})))
	t.Cleanup(func() { slog.SetDefault(old) })

	l := &logLines{name: "librespot", keep: librespotWorthLogging}
	at := time.Date(2026, 10, 31, 23, 13, 0, 0, time.UTC)
	key := func(stamp string) string {
		return "[" + stamp + " ERROR librespot_core::audio_key] error audio key 0 1"
	}
	l.log(key("2026-10-31T23:13:00Z"), at)
	l.log(key("2026-10-31T23:13:01Z"), at.Add(time.Second))
	l.log(key("2026-10-31T23:13:20Z"), at.Add(20*time.Second))
	if n := strings.Count(buf.String(), "error audio key"); n != 1 {
		t.Errorf("the same error within half a minute logged %d times, want 1:\n%s", n, buf.String())
	}
	if !strings.Contains(buf.String(), "level=WARN") {
		t.Errorf("not a warning:\n%s", buf.String())
	}
	l.log(key("2026-10-31T23:13:31Z"), at.Add(31*time.Second))
	if n := strings.Count(buf.String(), "error audio key"); n != 2 {
		t.Errorf("after half a minute logged %d times in all, want 2", n)
	}

	buf.Reset()
	(&logLines{name: "shairport-sync"}).log("a chatty line", at)
	if buf.Len() != 0 {
		t.Errorf("an unfiltered program's line reached Info:\n%s", buf.String())
	}
}
