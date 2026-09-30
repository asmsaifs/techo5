//go:build live

package ical

import (
	"net/http"
	"os"
	"testing"
	"time"
)

// Reads a real calendar, to see it the way a device would:
//
//	TECHO5_ICS=https://... go test -tags live -run TestLiveCalendar ./internal/lib/ical/
func TestLiveCalendar(t *testing.T) {
	addr := os.Getenv("TECHO5_ICS")
	if addr == "" {
		t.Skip("TECHO5_ICS is not set")
	}
	resp, err := http.Get(addr)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	now := time.Now()
	evs, err := Events(resp.Body, now.AddDate(0, -1, 0), now.AddDate(0, 3, 0))
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range evs {
		t.Logf("%s  %-5v  %s", e.Start.Format("Mon Jan 2 15:04"), e.AllDay, e.Summary)
	}
	if len(evs) == 0 {
		t.Error("no events in four months")
	}
}
