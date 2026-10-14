package detect

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/HuskerMinion/techo5/echod/internal/feature/voice"
)

// testClips is a recorder that saves into a list instead of onto the device.
func testClips(t *testing.T, on bool) (*clips, *[]clip) {
	t.Helper()
	c := newClips(t.TempDir())
	c.on.Store(on)
	c.shortest = 0 // a few samples stand for a clip
	c.blocked = func() bool { return false }
	c.after = func(time.Duration, func()) {} // settled by hand
	var saved []clip
	c.save = func(cl clip) { saved = append(saved, cl) }
	return c, &saved
}

func TestTheRingKeepsTheLastSecondsInOrder(t *testing.T) {
	c, _ := testClips(t, true)
	c.ring = make([]int16, 5)
	c.frame([]int16{1, 2, 3})
	if got := fmt.Sprint(c.snapshot()); got != "[1 2 3]" {
		t.Fatalf("before going round: %s", got)
	}
	c.frame([]int16{4, 5, 6, 7})
	if got := fmt.Sprint(c.snapshot()); got != "[3 4 5 6 7]" {
		t.Fatalf("after going round: %s, want the newest five oldest first", got)
	}
}

// Off, nothing is heard into the ring at all, and nothing is saved.
func TestNothingIsKeptWhileOff(t *testing.T) {
	c, saved := testClips(t, false)
	c.frame([]int16{1, 2, 3})
	if len(c.snapshot()) != 0 {
		t.Fatalf("the ring took audio while off: %v", c.snapshot())
	}
	c.scored(0, 0.95, 0.9)
	c.nearMiss(0, 0.8)
	c.settle()
	if len(*saved) != 0 {
		t.Fatalf("saved %+v while off", *saved)
	}
}

// A wake its turn calls a mistake is saved as one: "mistake" when nothing was said, "noise" when only
// noise was. One that was not stays pending until its time is up, and is then saved as meant.
func TestAWakeIsSavedAsAMistakeOrAsMeant(t *testing.T) {
	c, saved := testClips(t, true)
	c.model = func(slot int) string { return fmt.Sprintf("word%d", slot+1) }
	c.frame([]int16{1, 2})

	c.scored(0, 0.95, 0.9)
	c.mistaken(voice.Mistake{Slot: 0, Heard: "Thank you."})
	c.scored(1, 0.93, 0.9)
	c.mistaken(voice.Mistake{Slot: 1})
	if len(*saved) != 2 || (*saved)[0].kind != "noise" || (*saved)[0].peak != 0.95 || (*saved)[1].kind != "mistake" {
		t.Fatalf("after two mistakes: %+v", *saved)
	}
	if (*saved)[0].model != "word1" || (*saved)[1].model != "word2" || (*saved)[0].cutoff != 0.9 {
		t.Fatalf("the model and cutoff were not kept: %+v", *saved)
	}

	c.scored(0, 0.99, 0.9)
	if len(*saved) != 2 {
		t.Fatalf("a wake was saved before its turn could say: %+v", *saved)
	}
	c.settle()
	if len(*saved) != 3 || (*saved)[2].kind != "wake" || (*saved)[2].peak != 0.99 {
		t.Fatalf("the wake that was not a mistake: %+v", *saved)
	}
}

// A wake meant before the turn that was a mistake opened is not that turn's: it stays as it was.
func TestAnEarlierWakeIsNotTheMistakes(t *testing.T) {
	c, saved := testClips(t, true)
	at := time.Now()
	c.now = func() time.Time { return at }
	c.scored(0, 0.99, 0.9) // "Okay Nabu, turn on the lights"
	at = at.Add(10 * time.Second)
	opened := at
	at = at.Add(300 * time.Millisecond)
	c.scored(0, 0.91, 0.9) // the television
	c.mistaken(voice.Mistake{Slot: 0, Heard: "Thank you.", Opened: opened})
	if len(*saved) != 1 || (*saved)[0].peak != 0.91 || (*saved)[0].kind != "noise" {
		t.Fatalf("after the mistake: %+v", *saved)
	}
	c.settle()
	if len(*saved) != 2 || (*saved)[1].peak != 0.99 || (*saved)[1].kind != "wake" {
		t.Fatalf("the wake meant before it: %+v", *saved)
	}
}

// A wake heard again while its turn listens belongs to that turn: a mistake makes both mistakes.
func TestAMistakeTakesEveryWakeOfItsTurn(t *testing.T) {
	c, saved := testClips(t, true)
	c.scored(0, 0.92, 0.9)
	c.scored(0, 0.96, 0.9)
	c.mistaken(voice.Mistake{Slot: 0})
	if len(*saved) != 2 || (*saved)[0].kind != "mistake" || (*saved)[1].kind != "mistake" {
		t.Fatalf("two wakes of one mistaken turn: %+v", *saved)
	}
}

// What was heard before the microphones were cut is not spliced onto what comes after.
func TestMutingEmptiesTheRing(t *testing.T) {
	c, _ := testClips(t, true)
	c.frame([]int16{1, 2, 3})
	c.muted(true)
	c.frame([]int16{4})
	if got := fmt.Sprint(c.snapshot()); got != "[4]" {
		t.Fatalf("after a mute: %s, want only what came after it", got)
	}
}

// A mistake told of the other slot, or of a turn opened long after, is not this wake's.
func TestAMistakeHasToBeTheWakesOwn(t *testing.T) {
	c, saved := testClips(t, true)
	at := time.Now()
	c.now = func() time.Time { return at }
	c.scored(0, 0.95, 0.9)
	c.mistaken(voice.Mistake{Slot: 1})
	at = at.Add(mistakeWithin + time.Second)
	c.mistaken(voice.Mistake{Slot: 0, Opened: at})
	if len(*saved) != 0 {
		t.Fatalf("saved %+v for mistakes that were not this wake's", *saved)
	}
}

// Nothing is kept for the stop word, nor for the four seconds after it.
func TestTheStopWordIsNeverKept(t *testing.T) {
	c, saved := testClips(t, true)
	c.cutoff = func(int) float64 { return 0.9 }
	at := time.Now()
	c.now = func() time.Time { return at }
	c.scored(StopSlot, 0.95, 0.7)
	c.settle()
	if len(*saved) != 0 {
		t.Fatalf("the stop word saved %+v", *saved)
	}

	// The four seconds after it are in every clip, so nothing is kept until they have gone by.
	c.nearMiss(0, 0.8)
	at = at.Add(clipSeconds * time.Second)
	c.nearMiss(0, 0.8)
	if len(*saved) != 0 {
		t.Fatalf("a clip with the stop word in it was kept: %+v", *saved)
	}
	at = at.Add(time.Millisecond)
	c.nearMiss(0, 0.8)
	if len(*saved) != 1 || (*saved)[0].kind != "nearmiss" || (*saved)[0].cutoff != 0.9 {
		t.Fatalf("a near miss after the quiet: %+v", *saved)
	}
}

// A call, the intercom or a talk through a camera has the room: nothing heard then is kept.
func TestNothingIsKeptWhileSomethingElseHasTheRoom(t *testing.T) {
	c, saved := testClips(t, true)
	c.blocked = func() bool { return true }
	c.scored(0, 0.95, 0.9)
	c.nearMiss(0, 0.8)
	c.settle()
	if len(*saved) != 0 {
		t.Fatalf("saved %+v during a call", *saved)
	}
}

// Turned off, every clip goes, and the one pending with it; nothing is heard until it is back on.
func TestTurningItOffLetsEverythingGo(t *testing.T) {
	c, saved := testClips(t, true)
	c.write(clip{kind: "wake", at: time.Now(), pcm: []int16{1}})
	c.scored(0, 0.95, 0.9)
	c.enable(false)
	if _, err := os.Stat(c.dir); !os.IsNotExist(err) {
		t.Fatalf("the clips are still there: %v", err)
	}
	c.settle()
	if len(*saved) != 0 {
		t.Fatalf("the pending wake was kept after turning it off: %+v", *saved)
	}
	c.enable(true)
	if len(c.snapshot()) != 0 {
		t.Fatalf("the ring kept audio across off and on: %v", c.snapshot())
	}
}

// A clip written as the switch goes off is not left behind.
func TestAClipWrittenWhileOffIsNotKept(t *testing.T) {
	c, _ := testClips(t, false)
	c.write(clip{kind: "wake", at: time.Now(), peak: 0.99, pcm: []int16{1}})
	left, _ := filepath.Glob(filepath.Join(c.dir, "wake", "*"))
	if len(left) != 0 {
		t.Fatalf("left behind: %v", left)
	}
	// Nor are the folders the purge took away made again.
	if _, err := os.Stat(filepath.Join(c.dir, "wake")); !os.IsNotExist(err) {
		t.Fatalf("the folder was made while off: %v", err)
	}
}

// A clip whose write is waiting on a purge looks at the switch only once the purge is done: turned off
// in the meantime, it leaves no folder behind, empty or not.
func TestAClipWaitingOnAPurgeLeavesNoFolder(t *testing.T) {
	c, _ := testClips(t, true)
	c.disk.Lock() // a purge under way
	done := make(chan struct{})
	go func() {
		c.write(clip{kind: "wake", at: time.Now(), peak: 0.99, pcm: []int16{1}})
		close(done)
	}()
	time.Sleep(50 * time.Millisecond) // the write is waiting for disk
	c.on.Store(false)
	c.disk.Unlock()
	<-done
	if _, err := os.Stat(filepath.Join(c.dir, "wake")); !os.IsNotExist(err) {
		t.Fatalf("a folder was left behind: %v", err)
	}
}

// Off, the actions hand nothing out, not even what a failed purge left behind.
func TestTheActionsAnswerNothingWhileOff(t *testing.T) {
	c, _ := testClips(t, true)
	c.write(clip{kind: "wake", at: time.Now(), peak: 0.99, pcm: []int16{1}})
	got, err := c.list("", 0)
	if err != nil || got.Total != 1 {
		t.Fatalf("on: %+v, %v", got, err)
	}
	id := got.Clips[0].ID
	c.on.Store(false)
	if _, err := c.list("", 0); err == nil {
		t.Error("listed while off")
	}
	if _, err := c.audio(id, 0); err == nil {
		t.Error("fetched while off")
	}
}

// Two clips in one second are two files, named in the order they came.
func TestTwoClipsInOneSecondAreBothKept(t *testing.T) {
	c, _ := testClips(t, true)
	at := time.Date(2026, 10, 12, 21, 33, 1, 0, time.UTC)
	c.write(clip{kind: "nearmiss", at: at.Add(980 * time.Millisecond), peak: 0.8, pcm: []int16{1}})
	c.write(clip{kind: "nearmiss", at: at.Add(123 * time.Millisecond), peak: 0.8, pcm: []int16{1}})
	left, _ := filepath.Glob(filepath.Join(c.dir, "nearmiss", "*.wav"))
	if len(left) != 2 || filepath.Base(left[0]) != "20261012-213301-123-s1-p0.80.wav" ||
		filepath.Base(left[1]) != "20261012-213301-980-s1-p0.80.wav" {
		t.Fatalf("two in one second: %v", left)
	}
}

// The sensor says "near miss", so wake_clips takes that for nearmiss too.
func TestNearMissIsAKindAsTheSensorWritesIt(t *testing.T) {
	c, _ := testClips(t, true)
	c.write(clip{kind: "nearmiss", at: time.Now(), peak: 0.8, pcm: []int16{1}})
	for _, kind := range []string{"nearmiss", "near miss"} {
		got, err := c.list(kind, 0)
		if err != nil || got.Total != 1 || got.Clips[0].Kind != "nearmiss" {
			t.Errorf("%q: %+v, %v", kind, got, err)
		}
	}
}

// Less than a second in the ring (just after a mute, a call or turning it on) is too little to keep.
func TestAShortClipIsNotKept(t *testing.T) {
	c, saved := testClips(t, true)
	c.shortest = 4
	c.ring = make([]int16, 8)
	c.frame([]int16{1, 2, 3})
	c.scored(0, 0.95, 0.9)
	c.nearMiss(0, 0.8)
	c.settle()
	if len(*saved) != 0 {
		t.Fatalf("kept %+v from three samples", *saved)
	}
	c.frame([]int16{4})
	c.nearMiss(0, 0.8)
	if len(*saved) != 1 {
		t.Fatalf("four samples were not kept: %+v", *saved)
	}
}

// Nothing heard during a call or a talk through a camera is in a clip: the ring is emptied as it
// starts and as it ends, takes nothing in between, and nothing is kept for four seconds after either.
func TestNothingHeardDuringACallIsKept(t *testing.T) {
	c, saved := testClips(t, true)
	busy := false
	c.blocked = func() bool { return busy }
	at := time.Now()
	c.now = func() time.Time { return at }
	c.frame([]int16{1, 2})

	busy = true
	c.roomChanged()
	if len(c.snapshot()) != 0 {
		t.Fatalf("the ring kept what came before the call: %v", c.snapshot())
	}
	c.frame([]int16{3, 4})
	if len(c.snapshot()) != 0 {
		t.Fatalf("the ring took the call: %v", c.snapshot())
	}

	// The call ends, but the phone has not said so yet: still nothing.
	busy = false
	c.nearMiss(0, 0.8)
	if len(*saved) != 0 {
		t.Fatalf("kept %+v before the call's end was told", *saved)
	}

	at = at.Add(time.Minute)
	c.roomChanged()
	c.frame([]int16{5})
	c.nearMiss(0, 0.8)
	at = at.Add(clipSeconds * time.Second)
	c.nearMiss(0, 0.8)
	if len(*saved) != 0 {
		t.Fatalf("kept %+v in the four seconds after the call", *saved)
	}
	at = at.Add(time.Millisecond)
	c.nearMiss(0, 0.8)
	if len(*saved) != 1 || fmt.Sprint((*saved)[0].pcm) != "[5]" {
		t.Fatalf("after the call: %+v, want only what came after it", *saved)
	}

	// Told again with nothing changed (the talkback's countdown), nothing is emptied.
	c.roomChanged()
	if got := fmt.Sprint(c.snapshot()); got != "[5]" {
		t.Fatalf("a change that was none emptied the ring: %s", got)
	}
}

func TestAClipIsAWaveFileWithItsScoreAndTheOldestGo(t *testing.T) {
	c, _ := testClips(t, true)
	at := time.Date(2026, 10, 12, 21, 33, 0, 0, time.UTC)
	for i := range 3 {
		c.write(clip{kind: "nearmiss", at: at.Add(time.Duration(i) * time.Second), model: "okay_nabu",
			peak: 0.79, cutoff: 0.9, pcm: []int16{1, -1}})
	}
	b, err := os.ReadFile(filepath.Join(c.dir, "nearmiss", "20261012-213302-000-s1-p0.79-c0.90.wav"))
	if err != nil {
		t.Fatal(err)
	}
	want := []byte("RIFF\x28\x00\x00\x00WAVEfmt \x10\x00\x00\x00\x01\x00\x01\x00\x80\x3e\x00\x00" +
		"\x00\x7d\x00\x00\x02\x00\x10\x00data\x04\x00\x00\x00\x01\x00\xff\xff")
	if !bytes.Equal(b, want) {
		t.Fatalf("not 16 kHz mono 16-bit PCM holding 1, -1:\n got %x\nwant %x", b, want)
	}
	meta, err := os.ReadFile(filepath.Join(c.dir, "nearmiss", "20261012-213302-000-s1-p0.79-c0.90.json"))
	if err != nil {
		t.Fatal(err)
	}
	if got := string(meta); got != `{"kind":"nearmiss","at":"2026-10-12T21:33:02Z","slot":1,"model":"okay_nabu","score":0.79,"cutoff":0.9}` {
		t.Fatalf("the sidecar: %s", got)
	}

	prune(filepath.Join(c.dir, "nearmiss"), 2)
	left, _ := filepath.Glob(filepath.Join(c.dir, "nearmiss", "*"))
	if len(left) != 4 || filepath.Base(left[0]) != "20261012-213301-000-s1-p0.79-c0.90.json" {
		t.Fatalf("after pruning to two: %v", left)
	}
}

// wake_clips lists the clips newest first with what was made of each, and wake_clip_audio hands one
// back as the WAVE file it is.
func TestTheClipsAreListedAndFetched(t *testing.T) {
	c, _ := testClips(t, true)
	at := time.Date(2026, 10, 12, 21, 33, 0, 0, time.UTC)
	c.write(clip{kind: "wake", at: at, model: "okay_nabu", peak: 0.97, cutoff: 0.9, pcm: []int16{1}})
	c.write(clip{kind: "mistake", at: at.Add(time.Minute), slot: 1, model: "hey_jarvis", peak: 0.91, cutoff: 0.9, pcm: []int16{2}})
	// A clip still being written has no sidecar yet, and is not offered.
	os.WriteFile(filepath.Join(c.dir, "wake", "20261012-213500-s1-p0.99-c0.90.wav"), nil, 0o600)

	got, err := c.list("", 0)
	if err != nil {
		t.Fatal(err)
	}
	blob, _ := json.Marshal(got)
	want := `{"version":1,"page":0,"pages":1,"total":2,"clips":[` +
		`{"id":"mistake/20261012-213400-000-s2-p0.91-c0.90","kind":"mistake","at":"2026-10-12T21:34:00Z","slot":2,"model":"hey_jarvis","score":0.91,"cutoff":0.9},` +
		`{"id":"wake/20261012-213300-000-s1-p0.97-c0.90","kind":"wake","at":"2026-10-12T21:33:00Z","slot":1,"model":"okay_nabu","score":0.97,"cutoff":0.9}]}`
	if string(blob) != want {
		t.Fatalf("listed\n got %s\nwant %s", blob, want)
	}
	if got, _ := c.list("wake", 0); got.Total != 1 || got.Clips[0].Kind != "wake" {
		t.Fatalf("only the wakes: %+v", got)
	}
	if _, err := c.list("other", 0); err == nil {
		t.Fatal("a kind that is not one was listed")
	}

	a, err := c.audio(got.Clips[0].ID, 0)
	if err != nil {
		t.Fatal(err)
	}
	wav, _ := base64.StdEncoding.DecodeString(a.Data)
	if a.Pages != 1 || a.MIME != "audio/wav" || !bytes.Equal(wav, wave([]int16{2})) {
		t.Fatalf("the audio: %+v", a)
	}
	for _, id := range []string{"wake", "near miss/x", "../wake/x", "wake/../../state", "recordings/x", "wake/nothing"} {
		if _, err := c.audio(id, 0); err == nil {
			t.Errorf("%q was fetched", id)
		}
	}
}

// The sensors follow what is on the device: what an earlier run kept, each clip written, the oldest
// pruned, and nothing once it is turned off.
func TestTheSensorsCountTheClips(t *testing.T) {
	dir := t.TempDir()
	at := time.Date(2026, 10, 12, 21, 33, 0, 0, time.UTC)
	for i := range 2 {
		os.MkdirAll(filepath.Join(dir, "wake-clips", "nearmiss"), 0o700)
		os.WriteFile(filepath.Join(dir, "wake-clips", "nearmiss", fmt.Sprintf("2026101%d-000000-s1-p0.80.wav", i)), nil, 0o600)
	}
	c := newClips(filepath.Join(dir, "wake-clips"))
	c.on.Store(true)
	c.keep = 3
	var total int
	var kinds string
	c.publish = func(n int, k string) { total, kinds = n, k }

	c.recount()
	if total != 2 || kinds != "mistake 0 · noise 0 · near miss 2 · wake 0" {
		t.Fatalf("at the start: %d, %q", total, kinds)
	}
	for i := range 3 {
		c.write(clip{kind: "wake", at: at.Add(time.Duration(i) * time.Second), peak: 0.99, pcm: []int16{1}})
	}
	c.write(clip{kind: "mistake", at: at, peak: 0.95, pcm: []int16{1}})
	if total != 6 || kinds != "mistake 1 · noise 0 · near miss 2 · wake 3" {
		t.Fatalf("after four written: %d, %q", total, kinds)
	}
	c.write(clip{kind: "wake", at: at.Add(time.Minute), peak: 0.99, pcm: []int16{1}})
	if total != 6 || kinds != "mistake 1 · noise 0 · near miss 2 · wake 3" {
		t.Fatalf("a fourth wake past keep should prune the oldest: %d, %q", total, kinds)
	}
	c.enable(false)
	if total != 0 || kinds != "mistake 0 · noise 0 · near miss 0 · wake 0" {
		t.Fatalf("after turning it off: %d, %q", total, kinds)
	}
}
