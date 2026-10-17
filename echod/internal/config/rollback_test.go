package config

import (
	"os"
	"path/filepath"
	"testing"
)

// failWrites makes the store's writes fail (its temporary file is in the way as a folder) until the
// returned func is called.
func failWrites(t *testing.T, path string) func() {
	t.Helper()
	if err := os.Mkdir(path+".tmp", 0o755); err != nil {
		t.Fatal(err)
	}
	return func() {
		if err := os.Remove(path + ".tmp"); err != nil {
			t.Fatal(err)
		}
	}
}

// A change that could not be saved is taken back, so the next change saved does not write it to disk
// behind everyone's back: Keep wake clips turned on while the flash was failing stays off.
func TestAChangeThatCouldNotBeSavedIsTakenBack(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	st, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	unblock := failWrites(t, path)
	if err := st.Set().KeepClips(true); err == nil {
		t.Fatal("a write that could not happen said it saved")
	}
	if st.Get().Wake.KeepClips {
		t.Fatal("Keep wake clips that failed to save is on in memory")
	}
	unblock()
	if err := st.Set().NoneChosen(true); err != nil {
		t.Fatal(err)
	}
	again, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if again.Get().Wake.KeepClips {
		t.Fatal("an unrelated save wrote the Keep wake clips that had failed")
	}
	if !again.Get().Wake.NoneChosen {
		t.Fatal("the unrelated save did not hold")
	}
}

// Taken back whole: a change made in place inside a list (an alarm edited) goes too.
func TestAFailedChangeInAListIsTakenBack(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	st, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := st.Update(func(c *Config) { c.Alarms.List = []Alarm{{ID: "a", Label: "Work", RingOn: []string{"x"}}} }); err != nil {
		t.Fatal(err)
	}
	unblock := failWrites(t, path)
	defer unblock()
	if err := st.UpdateOrUndo(func(c *Config) {
		c.Alarms.List[0].Label = "Changed"
		c.Alarms.List[0].RingOn[0] = "y"
	}); err == nil {
		t.Fatal("a write that could not happen said it saved")
	}
	got := st.Get().Alarms.List
	if len(got) != 1 || got[0].Label != "Work" || got[0].RingOn[0] != "x" {
		t.Fatalf("after a failed edit the list is %+v", got)
	}
}

// A file that could not be read is never written over, so a change holds in memory for the run: there
// is nothing on disk for it to leak into, and taking it back would make every setting dead until the
// next start.
func TestAChangeHoldsWhenTheFileWasUnreadable(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	if err := os.WriteFile(path, []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	st, _ := Load(path)
	if err := st.Set().KeepClips(true); err == nil {
		t.Fatal("wrote over a file that could not be read")
	}
	if !st.Get().Wake.KeepClips {
		t.Fatal("a change was taken back where nothing is ever written")
	}
}

// Other settings hold in memory when the file cannot take them, as they always did: a value made up
// once (the DLNA renderer's id) must stay the same all run, saved or not.
func TestAnUpdateThatCouldNotBeSavedStillHolds(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	st, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	unblock := failWrites(t, path)
	defer unblock()
	if err := st.Update(func(c *Config) { c.Streaming.DLNAID = "uuid:1" }); err == nil {
		t.Fatal("a write that could not happen said it saved")
	}
	if st.Get().Streaming.DLNAID != "uuid:1" {
		t.Fatal("an id that could not be saved did not hold")
	}
}

// A Keep wake clips off that could not be saved holds, as the recorder has stopped for it, and the next
// change saved takes it to disk.
func TestAKeepClipsOffThatCouldNotBeSavedHolds(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	st, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := st.Set().KeepClips(true); err != nil {
		t.Fatal(err)
	}
	unblock := failWrites(t, path)
	if err := st.Set().KeepClips(false); err == nil {
		t.Fatal("a write that could not happen said it saved")
	}
	unblock()
	if err := st.Set().NoneChosen(true); err != nil {
		t.Fatal(err)
	}
	again, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if again.Get().Wake.KeepClips {
		t.Fatal("the off that could not be saved did not reach the disk with the next save")
	}
}

// A save whose new file is in place but whose folder could not be flushed after is on disk: the change
// is kept in memory to match, not taken back, and reported saved, so the caller acts on it too.
func TestAChangeOnDiskIsNotTakenBack(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "state.json")
	st, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	// Writable and searchable but not readable: the file can be made and renamed, the folder not opened.
	if err := os.Chmod(dir, 0o300); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.Chmod(dir, 0o700) }()
	if d, err := os.Open(dir); err == nil {
		_ = d.Close()
		t.Skip("the folder opens anyway (running as root?)")
	}
	if err := st.Set().KeepClips(true); err != nil {
		t.Fatalf("a change that reached the disk was reported failed: %v", err)
	}
	if !st.Get().Wake.KeepClips {
		t.Fatal("a change already on disk was taken back")
	}
	_ = os.Chmod(dir, 0o700)
	again, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if !again.Get().Wake.KeepClips {
		t.Fatal("the change was not on disk")
	}
}
