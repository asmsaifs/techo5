package home

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/HuskerMinion/techo5/echod/internal/config"
	"github.com/HuskerMinion/techo5/echod/internal/lib/hass"
	"github.com/HuskerMinion/techo5/echod/internal/lib/ical"
)

// Calendars read from their iCal address (config.Calendar.Links) rather than from Home Assistant, for
// a device that has none, or a calendar Home Assistant does not have. Each is a source like Home
// Assistant's, so the pages, the popups and the voice assistant see no difference.

// icalMax bounds a calendar file: a family calendar is a few hundred kilobytes.
const icalMax = 16 << 20

var icalClient = &http.Client{Timeout: 30 * time.Second}

// icalFiles keeps each address's file for as long as a month's events are kept, so reading this month
// and next does not fetch it twice.
var icalFiles struct {
	sync.Mutex
	body map[string][]byte
	at   map[string]time.Time
}

// linkCalendars is the Links as calendars.
func linkCalendars() []hass.Calendar {
	var out []hass.Calendar
	for _, l := range config.Get().Calendar.Links {
		out = append(out, hass.Calendar{ID: config.CalendarLinkPrefix + l.ID, Name: l.Name})
	}
	return out
}

// isLink is whether a source is one of the Links.
func isLink(src string) bool { return strings.HasPrefix(src, config.CalendarLinkPrefix) }

// linkEvents is one Links calendar's events between from and to.
func linkEvents(src string, from, to time.Time) ([]hass.Event, error) {
	id := strings.TrimPrefix(src, config.CalendarLinkPrefix)
	var link *config.CalendarLink
	for _, l := range config.Get().Calendar.Links {
		if l.ID == id {
			link = &l
			break
		}
	}
	if link == nil {
		return nil, fmt.Errorf("calendar: no link %s", id)
	}
	body, err := icalFile(link.URL)
	if err != nil {
		return nil, err
	}
	evs, err := ical.Events(bytes.NewReader(body), from, to)
	if err != nil {
		return nil, err
	}
	out := make([]hass.Event, len(evs))
	for i, e := range evs {
		out[i] = hass.Event{Calendar: src, Summary: e.Summary, Description: e.Description, Location: e.Location,
			Start: e.Start, End: e.End, AllDay: e.AllDay}
	}
	return out, nil
}

func icalFile(addr string) ([]byte, error) {
	icalFiles.Lock()
	if b, ok := icalFiles.body[addr]; ok && time.Since(icalFiles.at[addr]) < eventsEvery {
		icalFiles.Unlock()
		return b, nil
	}
	icalFiles.Unlock()

	// webcal:// is Apple's name for the same address over https.
	u := addr
	if rest, ok := strings.CutPrefix(u, "webcal://"); ok {
		u = "https://" + rest
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "TECHO5 (github.com/HuskerMinion/techo5)")
	resp, err := icalClient.Do(req)
	if err != nil {
		// The address is a secret: the error names the host at most, never the whole of it.
		return nil, fmt.Errorf("calendar: fetching from %s failed", req.URL.Host)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("calendar: %s answered %s", req.URL.Host, resp.Status)
	}
	b, err := io.ReadAll(io.LimitReader(resp.Body, icalMax))
	if err != nil {
		return nil, err
	}
	icalFiles.Lock()
	if icalFiles.body == nil {
		icalFiles.body, icalFiles.at = map[string][]byte{}, map[string]time.Time{}
	}
	icalFiles.body[addr], icalFiles.at[addr] = b, time.Now()
	icalFiles.Unlock()
	return b, nil
}

// forgetICalFile drops what is kept for an address, as when its link is removed.
func forgetICalFile(addr string) {
	icalFiles.Lock()
	delete(icalFiles.body, addr)
	delete(icalFiles.at, addr)
	icalFiles.Unlock()
}

// CheckCalendarLink fetches an address and reads it, to say before it is kept whether it is a calendar.
// It returns how many events it has in the coming month.
func CheckCalendarLink(addr string) (int, error) {
	body, err := icalFile(addr)
	if err != nil {
		return 0, err
	}
	if !bytes.Contains(body[:min(len(body), 4096)], []byte("BEGIN:VCALENDAR")) {
		forgetICalFile(addr)
		return 0, fmt.Errorf("calendar: that address did not answer with a calendar")
	}
	now := time.Now()
	evs, err := ical.Events(bytes.NewReader(body), now, now.AddDate(0, 1, 0))
	return len(evs), err
}
