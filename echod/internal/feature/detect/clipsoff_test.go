package detect

import (
	"errors"
	"path/filepath"
	"slices"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// clipsOffUnit is a Detect with only Keep wake clips: a recorder that saves into a list, the switch,
// the off record in a temporary folder, and a state save that fails while *fail is true.
func clipsOffUnit(t *testing.T, on bool) (*Detect, *bool, *[]bool) {
	t.Helper()
	oldPath, oldSave, oldEvery := clipsOffPath, saveKeepClips, clipsRetryEvery
	t.Cleanup(func() { clipsOffPath, saveKeepClips, clipsRetryEvery = oldPath, oldSave, oldEvery })
	clipsOffPath = filepath.Join(t.TempDir(), "keep-clips-off")
	clipsRetryEvery = time.Hour // the background retry never fires in a test; retryClipsOffOnce is called by hand
	fail := new(bool)
	var saves []bool
	saveKeepClips = func(on bool) error {
		if *fail {
			return errors.New("disk full")
		}
		saves = append(saves, on)
		return nil
	}
	c, _ := testClips(t, on)
	d := &Detect{clips: c}
	d.keepClips = newKeepClipsEntity(d)
	return d, fail, &saves
}

// The redditor's case: turned off, the save fails. Clips stop now, and the off is on record so the
// next start does not turn them back on.
func TestAnOffThatCannotBeSavedHoldsPastARestart(t *testing.T) {
	d, fail, saves := clipsOffUnit(t, true)
	*fail = true
	d.keepClips.OnCommand(false)
	if d.clips.on.Load() {
		t.Fatal("clips are still kept after Keep wake clips was turned off")
	}
	if !clipsOffMarked() {
		t.Fatal("the off was not recorded, so the next start would read the old on")
	}
	// The next start, with the state still saying on.
	if d.keepClipsAtStart(true) {
		t.Fatal("the next start turned Keep wake clips back on")
	}
	// Once a save holds, the record goes.
	*fail = false
	if !d.retryClipsOffOnce() {
		t.Fatal("a save that works still left something to do")
	}
	if clipsOffMarked() {
		t.Fatal("the off record stayed after the state took the off")
	}
	if len(*saves) != 1 || (*saves)[0] {
		t.Fatalf("saves %v, want one off", *saves)
	}
}

// At a start with an off on record, the off is saved at once when it can be.
func TestAStartSavesTheRecordedOff(t *testing.T) {
	d, _, saves := clipsOffUnit(t, false)
	if err := markClipsOff(); err != nil {
		t.Fatal(err)
	}
	if d.keepClipsAtStart(true) {
		t.Fatal("started with clips on over a recorded off")
	}
	if clipsOffMarked() || len(*saves) != 1 || (*saves)[0] {
		t.Fatalf("record left %v, saves %v: want the off saved and the record gone", clipsOffMarked(), *saves)
	}
}

// Turned on again and saved, that is the setting: the old off record must not win at the next start,
// and the retry stops.
func TestTurningOnAgainAndSavedClearsTheRecord(t *testing.T) {
	d, fail, _ := clipsOffUnit(t, true)
	*fail = true
	d.keepClips.OnCommand(false)
	*fail = false
	d.keepClips.OnCommand(true)
	if clipsOffMarked() {
		t.Fatal("a saved on left the old off record behind")
	}
	if !d.keepClipsAtStart(true) {
		t.Fatal("the next start ignored the saved on")
	}
	if !d.retryClipsOffOnce() {
		t.Fatal("the retry would go on saving off while clips are kept again")
	}
}

// Nothing on record: the saved setting is used as it is, either way.
func TestWithoutARecordTheSavedSettingStands(t *testing.T) {
	d, _, saves := clipsOffUnit(t, false)
	if !d.keepClipsAtStart(true) || d.keepClipsAtStart(false) {
		t.Fatal("the saved setting was not used")
	}
	if len(*saves) != 0 {
		t.Fatalf("saved %v with nothing to save", *saves)
	}
}

// An on that cannot be saved stays off, as before: nothing is kept on a promise the state never took.
func TestAnOnThatCannotBeSavedStaysOff(t *testing.T) {
	d, fail, _ := clipsOffUnit(t, false)
	*fail = true
	d.keepClips.OnCommand(true)
	if d.clips.on.Load() {
		t.Fatal("clips are kept although turning them on was not saved")
	}
}

// syncedSaves replaces the state save for a test whose saves come from more than one goroutine: each
// save is recorded in order, failing while fail is set, and before recording it calls during (nil for
// nothing) with what is being saved.
type syncedSaves struct {
	mu     sync.Mutex
	saves  []bool
	fail   atomic.Bool
	during func(on bool)
}

func (s *syncedSaves) use(t *testing.T) {
	t.Helper()
	old := saveKeepClips
	t.Cleanup(func() { saveKeepClips = old })
	saveKeepClips = func(on bool) error {
		if s.during != nil {
			s.during(on)
		}
		if s.fail.Load() {
			return errors.New("disk full")
		}
		s.mu.Lock()
		s.saves = append(s.saves, on)
		s.mu.Unlock()
		return nil
	}
}

func (s *syncedSaves) got() []bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return slices.Clone(s.saves)
}

// Turned off, the recorder has stopped before the save starts: nothing is kept while a save that is
// slow to fail runs.
func TestTurnedOffTheRecorderStopsBeforeTheSave(t *testing.T) {
	d, _, _ := clipsOffUnit(t, true)
	var s syncedSaves
	recording := false
	s.during = func(on bool) { recording = d.clips.on.Load() }
	s.use(t)
	d.keepClips.OnCommand(false)
	if recording {
		t.Fatal("clips were still being kept while the off was saved")
	}
}

// A retry of the off that is mid-save when Keep wake clips is turned on again cannot save its off over
// the on: the turn on waits for it, and is saved after it.
func TestARetryCannotSaveOffOverATurnOn(t *testing.T) {
	d, _, _ := clipsOffUnit(t, false)
	var s syncedSaves
	inside, release := make(chan struct{}), make(chan struct{})
	var once sync.Once
	s.during = func(on bool) {
		if !on {
			once.Do(func() { close(inside); <-release })
		}
	}
	s.use(t)

	retried := make(chan struct{})
	go func() { d.retryClipsOffOnce(); close(retried) }()
	<-inside
	turnedOn := make(chan struct{})
	go func() { d.keepClips.OnCommand(true); close(turnedOn) }()
	time.Sleep(50 * time.Millisecond) // the turn on, given its chance to go first
	close(release)
	<-retried
	<-turnedOn

	got := s.got()
	if len(got) == 0 || !got[len(got)-1] {
		t.Fatalf("saves %v: the retry's off was saved over the turn on", got)
	}
	if !d.clips.on.Load() {
		t.Fatal("clips are not kept after Keep wake clips was turned on")
	}
}

// An off that fails after the retry before it has ended gets a retry of its own.
func TestAnOffThatFailsAfterARetryEndedIsTriedAgain(t *testing.T) {
	d, _, _ := clipsOffUnit(t, true)
	clipsRetryEvery = 5 * time.Millisecond
	var s syncedSaves
	s.use(t)
	s.fail.Store(true)
	d.keepClips.OnCommand(false)
	s.fail.Store(false)
	waitFor(t, "the first off saved", func() bool { return !clipsOffMarked() && !d.retrying() })

	d.keepClips.OnCommand(true)
	s.fail.Store(true)
	d.keepClips.OnCommand(false)
	if !d.retrying() {
		t.Fatal("a second off that failed is not being tried again")
	}
	s.fail.Store(false)
	waitFor(t, "the second off saved", func() bool { return !clipsOffMarked() && !d.retrying() })
}

func (d *Detect) retrying() bool {
	d.clipsSave.Lock()
	defer d.clipsSave.Unlock()
	return d.clipsOffRetrying
}

func waitFor(t *testing.T, what string, ok func() bool) {
	t.Helper()
	for end := time.Now().Add(5 * time.Second); time.Now().Before(end); time.Sleep(5 * time.Millisecond) {
		if ok() {
			return
		}
	}
	t.Fatalf("timed out waiting for %s", what)
}

// A save that fails for good is logged for its first few failures, then every so often, not every
// minute forever.
func TestARetryThatKeepsFailingIsLoggedLessOften(t *testing.T) {
	now := time.Now()
	for fails := 1; fails <= clipsLogFirst; fails++ {
		if !logClipsRetry(fails, now, now) {
			t.Fatalf("failure %d was not logged", fails)
		}
	}
	if logClipsRetry(clipsLogFirst+1, now.Add(-time.Minute), now) {
		t.Fatal("a failure a minute after the last one logged was logged")
	}
	if !logClipsRetry(clipsLogFirst+10, now.Add(-clipsLogEvery), now) {
		t.Fatal("a failure long after the last one logged was not logged")
	}
}

// retryClipsOffOnce is one try of the retry, as its loop makes it, for the tests to call by hand.
func (d *Detect) retryClipsOffOnce() bool {
	d.clipsSave.Lock()
	defer d.clipsSave.Unlock()
	return d.retryClipsOffLocked()
}
