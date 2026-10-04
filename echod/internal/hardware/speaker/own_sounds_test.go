package speaker

import (
	"bytes"
	"encoding/binary"
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"
)

// ownSounds points SoundsDir at a folder of the test's own for as long as the test runs.
func ownSounds(t *testing.T) string {
	t.Helper()
	was := SoundsDir
	SoundsDir = t.TempDir()
	t.Cleanup(func() { SoundsDir = was })
	return SoundsDir
}

// writeWAV writes ms of a constant level as a 16-bit WAVE file at rate, with channels channels.
func writeWAV(t *testing.T, path string, rate, channels, ms int) {
	t.Helper()
	frames := rate * ms / 1000
	var b bytes.Buffer
	b.WriteString("RIFF")
	_ = binary.Write(&b, binary.LittleEndian, uint32(36+frames*channels*2))
	b.WriteString("WAVEfmt ")
	_ = binary.Write(&b, binary.LittleEndian, uint32(16))
	_ = binary.Write(&b, binary.LittleEndian, uint16(1))
	_ = binary.Write(&b, binary.LittleEndian, uint16(channels))
	_ = binary.Write(&b, binary.LittleEndian, uint32(rate))
	_ = binary.Write(&b, binary.LittleEndian, uint32(rate*channels*2))
	_ = binary.Write(&b, binary.LittleEndian, uint16(channels*2))
	_ = binary.Write(&b, binary.LittleEndian, uint16(16))
	b.WriteString("data")
	_ = binary.Write(&b, binary.LittleEndian, uint32(frames*channels*2))
	for range frames * channels {
		_ = binary.Write(&b, binary.LittleEndian, int16(8000))
	}
	if err := os.WriteFile(path, b.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
}

// A recording of your own named for a sound plays in its place, at its own length, and taking it
// away brings the stock sound back without a restart.
func TestARecordingOfYourOwnReplacesTheSound(t *testing.T) {
	dir := ownSounds(t)
	c := &Clip{file: "mute_switch_on"}
	stock := c.Ms()

	own := filepath.Join(dir, "mute_switch_on.wav")
	writeWAV(t, own, Rate, 2, 250)
	if c.Ms() != 250 || !c.Own() {
		t.Fatalf("with a recording of its own it lasts %d ms, own %v", c.Ms(), c.Own())
	}
	if n := len(tone(c.Note(), toneLevel)); n != Rate/4*Channels {
		t.Errorf("rendered %d samples, want %d", n, Rate/4*Channels)
	}
	if got := Length([]Note{c.Note()}); got.Milliseconds() != 250 {
		t.Errorf("its length is %v", got)
	}

	if err := os.Remove(own); err != nil {
		t.Fatal(err)
	}
	if c.Ms() != stock || c.Own() {
		t.Errorf("with the recording gone it lasts %d ms, want the stock %d", c.Ms(), stock)
	}
}

// A file the device cannot play as it is leaves the stock sound playing: a recording at the wrong rate
// played anyway is the right sound at the wrong speed.
func TestARecordingAtTheWrongRateIsNotPlayed(t *testing.T) {
	dir := ownSounds(t)
	c := &Clip{file: "mute_switch_off"}
	stock := c.Ms()
	writeWAV(t, filepath.Join(dir, "mute_switch_off.wav"), 44100, 2, 250)
	if c.Own() || c.Ms() != stock {
		t.Errorf("a 44.1 kHz recording was taken: own %v, %d ms against the stock %d", c.Own(), c.Ms(), stock)
	}
}

// Failure and cancel have no recording of their own to begin with, so they keep their notes until
// somebody gives them one.
func TestFailureAndCancelTakeARecording(t *testing.T) {
	dir := ownSounds(t)
	if len(FailureSound()) != len(ToneTrouble) || len(CancelSound()) != len(ToneCancel) {
		t.Fatal("with no recordings, failure and cancel are not their notes")
	}
	writeWAV(t, filepath.Join(dir, "failure.wav"), Rate, 1, 400)
	writeWAV(t, filepath.Join(dir, "canceled.wav"), Rate, 1, 300)
	if got := Length(FailureSound()); got.Milliseconds() != 400 {
		t.Errorf("failure lasts %v with a recording of its own", got)
	}
	if got := Length(CancelSound()); got.Milliseconds() != 300 {
		t.Errorf("cancel lasts %v with a recording of its own", got)
	}
}

// The 10-second limit is on how long a recording plays, whatever its channels: a 10-second stereo
// file is taken, and a 15-second mono one, though fewer bytes, is not.
func TestTheLimitIsTenSecondsOfSound(t *testing.T) {
	dir := ownSounds(t)
	c := &Clip{file: "timer_finished"}
	writeWAV(t, filepath.Join(dir, "timer_finished.wav"), Rate, 2, 10000)
	if !c.Own() || c.Ms() != 10000 {
		t.Errorf("a 10-second stereo recording was not taken: own %v, %d ms", c.Own(), c.Ms())
	}
	writeWAV(t, filepath.Join(dir, "timer_finished.wav"), Rate, 1, 15000)
	if c.Own() {
		t.Error("a 15-second mono recording was taken")
	}
}

// A recording with no sound in it leaves the stock sound playing rather than silence.
func TestAnEmptyRecordingIsNotPlayed(t *testing.T) {
	dir := ownSounds(t)
	c := &Clip{file: "mute_switch_on"}
	stock := c.Ms()
	writeWAV(t, filepath.Join(dir, "mute_switch_on.wav"), Rate, 1, 0)
	if c.Own() || c.Ms() != stock {
		t.Errorf("an empty recording was taken: own %v, %d ms against the stock %d", c.Own(), c.Ms(), stock)
	}
}

// Something in the folder that is not a file, a pipe say, is not read: reading a pipe waits for a
// writer that may never come, and the sound with it.
func TestAPipeIsNotRead(t *testing.T) {
	dir := ownSounds(t)
	c := &Clip{file: "mute_switch_off"}
	if err := syscall.Mkfifo(filepath.Join(dir, "mute_switch_off.wav"), 0o600); err != nil {
		t.Fatal(err)
	}
	own := make(chan bool, 1)
	go func() { own <- c.Own() }()
	select {
	case got := <-own:
		if got {
			t.Error("a pipe was taken for a recording")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("reading the pipe hung")
	}
}

// A recording replaced by another of the same size, with the old one's time put back as a copy that
// keeps times does (scp -p, cp -p), is still noticed.
func TestARecordingReplacedIsNoticed(t *testing.T) {
	dir := ownSounds(t)
	c := &Clip{file: "timer_finished"}
	own := filepath.Join(dir, "timer_finished.wav")
	writeWAV(t, own, Rate, 1, 500)
	if got := c.render(toneLevel); len(got) == 0 || got[0] <= 0 {
		t.Fatalf("the first recording did not play: %d samples", len(got))
	}
	info, err := os.Stat(own)
	if err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(own)
	if err != nil {
		t.Fatal(err)
	}
	for i := 44; i+1 < len(b); i += 2 {
		binary.LittleEndian.PutUint16(b[i:], uint16(0xffff-8000+1)) // -8000
	}
	tmp := own + ".new"
	if err := os.WriteFile(tmp, b, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(tmp, info.ModTime(), info.ModTime()); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(tmp, own); err != nil {
		t.Fatal(err)
	}
	if got := c.render(toneLevel); len(got) == 0 || got[0] >= 0 {
		t.Error("the old recording still plays")
	}
}

// A WAVE file that is not plain PCM is not played, whatever its fmt chunk says about bits.
func TestARecordingNotPCMIsNotPlayed(t *testing.T) {
	dir := ownSounds(t)
	c := &Clip{file: "mute_switch_off"}
	stock := c.Ms()
	own := filepath.Join(dir, "mute_switch_off.wav")
	writeWAV(t, own, Rate, 1, 250)
	b, err := os.ReadFile(own)
	if err != nil {
		t.Fatal(err)
	}
	binary.LittleEndian.PutUint16(b[20:], 3) // IEEE float
	if err := os.WriteFile(own, b, 0o644); err != nil {
		t.Fatal(err)
	}
	if c.Own() || c.Ms() != stock {
		t.Errorf("a recording that is not PCM was taken: own %v, %d ms", c.Own(), c.Ms())
	}
}

// The notes made as the program starts read no file of the owner's: one that cannot play is read the
// first time it is wanted, when there is a log to say why.
func TestStockNotesReadNothing(t *testing.T) {
	dir := ownSounds(t)
	c := &Clip{file: "timer_finished"}
	writeWAV(t, filepath.Join(dir, "timer_finished.wav"), 44100, 1, 250)
	n := c.stockNote()
	c.mu.Lock()
	loaded := c.loaded
	c.mu.Unlock()
	if loaded || n.Ms != c.stockMs() || n.Clip != c {
		t.Errorf("stock note %+v, read %v", n, loaded)
	}
}

// The failure sound with a recording of the owner's is that recording, at its length, from one look.
func TestFailureNoteHasItsOwnLength(t *testing.T) {
	dir := ownSounds(t)
	t.Cleanup(func() {
		// Read again on its next use, from the real folder.
		ClipFailure.mu.Lock()
		ClipFailure.loaded = false
		ClipFailure.mu.Unlock()
	})
	writeWAV(t, filepath.Join(dir, "failure.wav"), Rate, 1, 300)
	notes := FailureSound()
	if len(notes) != 1 || notes[0].Clip != ClipFailure || notes[0].Ms != 300 {
		t.Errorf("failure sound %+v", notes)
	}
}

// An extensible WAVE header is taken for what its sub-format says: PCM plays, anything else does not.
func TestExtensibleWAVE(t *testing.T) {
	wav := func(sub uint16) []byte {
		var b bytes.Buffer
		b.WriteString("RIFF")
		_ = binary.Write(&b, binary.LittleEndian, uint32(4+8+40+8+4))
		b.WriteString("WAVEfmt ")
		_ = binary.Write(&b, binary.LittleEndian, uint32(40))
		for _, v := range []any{uint16(0xFFFE), uint16(1), uint32(Rate), uint32(Rate * 2), uint16(2), uint16(16),
			uint16(22), uint16(16), uint32(4), sub} {
			_ = binary.Write(&b, binary.LittleEndian, v)
		}
		b.Write(make([]byte, 14)) // the rest of the sub-format GUID
		b.WriteString("data")
		_ = binary.Write(&b, binary.LittleEndian, uint32(4))
		_ = binary.Write(&b, binary.LittleEndian, []int16{100, 200})
		return b.Bytes()
	}
	if s, f, err := MonoWAV(wav(1)); err != nil || len(s) != 2 || f.Format != 1 {
		t.Errorf("extensible PCM: %v samples, format %d, %v", s, f.Format, err)
	}
	if _, _, err := MonoWAV(wav(3)); err == nil {
		t.Error("extensible float taken")
	}
}
