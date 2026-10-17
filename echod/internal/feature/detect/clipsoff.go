package detect

import (
	"errors"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"time"

	"github.com/HuskerMinion/techo5/echod/internal/config"
	"github.com/HuskerMinion/techo5/echod/internal/layout"
	"github.com/HuskerMinion/techo5/echod/internal/lib/safe"
)

// Keep wake clips fails closed. Turned off, the recorder stops and the clips go at once (clips.go),
// before the setting is saved. The setting is saved with the rest of the state, and a save that fails
// (a full or failing flash) used to leave "on" in the file: the next start read it and kept clips
// again, which nobody had asked for. So an off that cannot be saved is also recorded on its own, in
// clipsOffPath, a file next to the state that wins over it at the next start, and the save is tried
// again every clipsRetryEvery until it holds. The file goes when the off is saved, when Keep wake
// clips is turned on again and that is saved, or at a start that finds the state saying off already.
var (
	clipsOffPath    = filepath.Join(layout.StateDir, "keep-clips-off")
	saveKeepClips   = func(on bool) error { return config.Set().KeepClips(on) }
	clipsRetryEvery = time.Minute
)

// A retry that keeps failing is logged each time for the first clipsLogFirst failures in a row, and
// after that at most every clipsLogEvery: a flash that has failed for good would otherwise fill the
// log with the same line every minute.
const (
	clipsLogFirst = 3
	clipsLogEvery = 10 * time.Minute
)

// clipsOffMarked is whether an off that was never saved is on record.
func clipsOffMarked() bool {
	_, err := os.Stat(clipsOffPath)
	return err == nil
}

// markClipsOff records an off the state could not take, synced with the folder it is in, so a power
// cut right after it cannot lose it either.
func markClipsOff() error {
	f, err := os.OpenFile(clipsOffPath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
	if err != nil {
		return err
	}
	if _, err := f.WriteString("off\n"); err != nil {
		_ = f.Close()
		return err
	}
	if err := f.Sync(); err != nil {
		_ = f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	// A new file's name is in its folder, which has to reach the flash too (as config's store does).
	dir, err := os.Open(filepath.Dir(clipsOffPath))
	if err != nil {
		return err
	}
	defer func() { _ = dir.Close() }()
	return dir.Sync()
}

// clearClipsOff lets go of the record once the state holds the setting itself.
func clearClipsOff() {
	if err := os.Remove(clipsOffPath); err != nil && !errors.Is(err, fs.ErrNotExist) {
		slog.Warn("keep wake clips: removing the off record failed", "path", clipsOffPath, "err", err)
	}
}

// keepClipsAtStart is what Keep wake clips starts as: the saved setting, unless an off that never
// reached the state is on record, which wins. That off is saved now if it can be, and tried again
// otherwise.
func (d *Detect) keepClipsAtStart(saved bool) bool {
	if !clipsOffMarked() {
		return saved
	}
	if !saved {
		clearClipsOff()
		return false
	}
	slog.Warn("keep wake clips: turned off before the last stop but never saved; staying off")
	d.saveClipsOff()
	return false
}

// saveClipsOff saves an off the state did not take: now, and again every clipsRetryEvery until it
// holds or Keep wake clips is turned on again (which saves its own setting).
func (d *Detect) saveClipsOff() {
	d.clipsSave.Lock()
	defer d.clipsSave.Unlock()
	if !d.retryClipsOffLocked() {
		d.retryClipsOffLater()
	}
}

// retryClipsOffLater starts trying the off again every clipsRetryEvery, unless that already runs.
// Under clipsSave, as is the retry's decision to stop, so an off that fails just as the retry before
// it ends is never left without one.
func (d *Detect) retryClipsOffLater() {
	if d.clipsOffRetrying {
		return
	}
	d.clipsOffRetrying = true
	every := clipsRetryEvery
	safe.Go("keep wake clips off", func() {
		for {
			time.Sleep(every)
			d.clipsSave.Lock()
			done := d.retryClipsOffLocked()
			if done {
				d.clipsOffRetrying = false
			}
			d.clipsSave.Unlock()
			if done {
				return
			}
		}
	})
}

// retryClipsOffLocked tries the save once, with clipsSave held, and reports whether there is nothing
// left to do: saved, or no longer wanted because clips are being kept again. The look at whether clips
// are kept again and the save that follows it are one step, so a Keep wake clips turned on between them
// cannot be saved over with off.
func (d *Detect) retryClipsOffLocked() bool {
	if d.clips != nil && d.clips.on.Load() {
		d.clipsFails = 0
		return true
	}
	if err := saveKeepClips(false); err != nil {
		d.clipsFails++
		if logClipsRetry(d.clipsFails, d.clipsLogged, time.Now()) {
			d.clipsLogged = time.Now()
			slog.Error("saving keep wake clips off failed; trying again", "err", err, "after", clipsRetryEvery, "failures", d.clipsFails)
		}
		return false
	}
	d.clipsFails = 0
	clearClipsOff()
	slog.Info("keep wake clips: off saved")
	return true
}

// logClipsRetry is whether the fails-th failure in a row is logged, the last logged at logged.
func logClipsRetry(fails int, logged, now time.Time) bool {
	return fails <= clipsLogFirst || now.Sub(logged) >= clipsLogEvery
}
