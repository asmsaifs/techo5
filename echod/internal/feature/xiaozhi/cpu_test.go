package xiaozhi

import (
	"testing"
	"time"
)

func TestSessionIdleTracksLastMessage(t *testing.T) {
	s := &Session{}
	s.stats.Since = time.Now().Add(-time.Hour)
	if s.Idle() < time.Minute {
		t.Fatalf("a session with no messages should be idle since it opened, got %v", s.Idle())
	}
	s.count(1)
	if s.Idle() > time.Second {
		t.Fatalf("a message just arrived, idle = %v", s.Idle())
	}
}

func TestCPUMeterNeverNegative(t *testing.T) {
	m := newCPUMeter()
	if v := m.sample(); v < 0 {
		t.Fatalf("cpu = %v", v)
	}
}
