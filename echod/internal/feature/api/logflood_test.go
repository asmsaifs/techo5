package api

import (
	"bytes"
	"log/slog"
	"strings"
	"testing"
	"time"
)

// A flood of closed connections from one address keeps a few lines, one warning, and a count when
// the minute is up, said by the minute's own timer even when the flood has stopped; the first from
// another address still gets through; other lines pass untouched.
func TestFloodGuard(t *testing.T) {
	var buf bytes.Buffer
	now := time.Date(2026, 10, 13, 9, 0, 0, 0, time.UTC)
	var flush func()
	st := &floodState{now: func() time.Time { return now }, after: func(_ time.Duration, f func()) *time.Timer {
		flush = f
		return nil
	}}
	base := slog.NewTextHandler(&buf, nil)
	log := slog.New(floodGuard{Handler: base, root: base, st: st})
	for i := range 50 {
		log.With("peer", "192.0.2.20:"+string(rune('a'+i%26))).Info("connection closed", "err", "broken pipe")
		now = now.Add(50 * time.Millisecond)
	}
	log.With("peer", "192.0.2.30:4000").Info("connection closed", "err", "handshake")
	log.Info("something else")
	out := buf.String()
	if got := strings.Count(out, "msg=\"connection closed\""); got != closedSeen+1 {
		t.Errorf("%d closed lines passed, want %d (the first few, and the other address's first)", got, closedSeen+1)
	}
	if strings.Count(out, "keeps reconnecting") != 1 || strings.Contains(out, "home assistant") {
		t.Errorf("want one warning about a client: %s", out)
	}
	if !strings.Contains(out, "something else") {
		t.Error("another line was held back")
	}
	if flush == nil {
		t.Fatal("no timer for the minute's count")
	}
	now = now.Add(3 * time.Hour) // the timer is late: the count still says at most a minute
	flush()
	out = buf.String()
	if !strings.Contains(out, "closed=51") || !strings.Contains(out, "within=1m0s") || !strings.Contains(out, "last_err=handshake") {
		t.Errorf("no count for the minute: %s", out)
	}
	n := len(buf.String())
	flush()
	if len(buf.String()) != n {
		t.Error("the count said twice")
	}
}
