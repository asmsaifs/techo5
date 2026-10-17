package config

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// A snooze survives a restart, as the absolute time it was set for.
//
// It used to live only in memory, so a restart between pressing Snooze and the alarm coming back
// lost it without a word — and somebody who pressed Snooze has been told the alarm is coming back.
func TestSnoozesAreSavedAndReadBack(t *testing.T) {
	st := load(t)

	if got := st.Get().Alarms.Snoozed; len(got) != 0 {
		t.Fatalf("%d snoozes before any were saved", len(got))
	}

	at := time.Date(2026, 9, 16, 6, 39, 0, 0, time.UTC)
	want := []Snooze{
		{Key: "snooze:wake", Label: "Wake up", At: at},
		{Key: "snooze:pills", At: at.Add(20 * time.Minute)},
	}
	if err := st.Set().Alarms().Snoozed(want); err != nil {
		t.Fatalf("saving the snoozes: %v", err)
	}

	got := st.Get().Alarms.Snoozed
	if len(got) != len(want) {
		t.Fatalf("read back %d snoozes, want %d", len(got), len(want))
	}
	for i := range want {
		if got[i].Key != want[i].Key || got[i].Label != want[i].Label || !got[i].At.Equal(want[i].At) {
			t.Errorf("snooze %d read back as %+v, want %+v", i, got[i], want[i])
		}
	}

	// Canceling them all is an empty list, not an absent one.
	if err := st.Set().Alarms().Snoozed(nil); err != nil {
		t.Fatalf("clearing the snoozes: %v", err)
	}
	if got := st.Get().Alarms.Snoozed; len(got) != 0 {
		t.Errorf("%d snoozes after clearing them", len(got))
	}
}

// The alarm the wake light is held for survives a restart beside the snoozes (#161), and a file saved
// before there was one reads as none.
func TestTheHeldWakeLightIsSavedAndReadBack(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	if err := os.WriteFile(path, []byte(`{"alarms":{"snoozed":[{"key":"snooze:wake","at":"2026-10-16T08:09:00Z","lit":true}]}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	st, err := Load(path)
	if err != nil {
		t.Fatalf("an older file with no held light did not load: %v", err)
	}
	if got := st.Get().Alarms; got.Held != nil || len(got.Snoozed) != 1 {
		t.Fatalf("the older file read as held %+v and %d snoozes, want none and one", got.Held, len(got.Snoozed))
	}

	at := time.Date(2026, 10, 16, 8, 30, 0, 0, time.UTC)
	if err := st.Set().Alarms().Held(&Held{Key: "b", At: at}); err != nil {
		t.Fatalf("saving the held light: %v", err)
	}
	again, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if h := again.Get().Alarms.Held; h == nil || h.Key != "b" || !h.At.Equal(at) {
		t.Errorf("the held light read back as %+v, want b at %v", h, at)
	}

	if err := st.Set().Alarms().Held(nil); err != nil {
		t.Fatalf("letting the held light go: %v", err)
	}
	if h := st.Get().Alarms.Held; h != nil {
		t.Errorf("the held light is %+v after it was let go", h)
	}
}

// Get returns a copy. A caller that holds the snapshot and writes into it must not be writing into
// the store's own slice — every other slice on Config is cloned for exactly this reason.
func TestTheSavedSnoozesAreNotTheStoresOwnSlice(t *testing.T) {
	st := load(t)

	at := time.Date(2026, 9, 16, 6, 39, 0, 0, time.UTC)
	if err := st.Set().Alarms().Snoozed([]Snooze{{Key: "snooze:wake", Label: "Wake up", At: at}}); err != nil {
		t.Fatalf("saving the snoozes: %v", err)
	}

	held := st.Get().Alarms.Snoozed
	held[0].Label = "scribbled on"

	if got := st.Get().Alarms.Snoozed[0].Label; got != "Wake up" {
		t.Errorf("the store's snooze now reads %q; Get handed out its own slice", got)
	}
}
