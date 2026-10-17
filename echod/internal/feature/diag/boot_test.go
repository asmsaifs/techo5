package diag

import (
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	esphome "github.com/ygelfand/go-esphome-device"

	"github.com/HuskerMinion/techo5/echod/internal/hardware/metrics"
)

// btime writes a /proc/stat with the given boot time and returns a reader over it.
func btime(t *testing.T, secs int64) metrics.Reader {
	t.Helper()
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "proc"), 0o755); err != nil {
		t.Fatal(err)
	}
	body := "cpu  1 2 3 4\nbtime " + strconv.FormatInt(secs, 10) + "\n"
	if err := os.WriteFile(filepath.Join(root, "proc/stat"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return metrics.Reader{Root: root}
}

// The boot time goes out as a timestamp Home Assistant can parse, and the second's wobble the kernel's
// figure has as the clock is disciplined is not a new state; a real move, such as the clock being set,
// is.
func TestBootIsATimestampThatDoesNotWobble(t *testing.T) {
	d := &Diag{booted: &esphome.TextSensor{}}

	const at = 1760000000
	d.boot(btime(t, at))
	want := time.Unix(at, 0).UTC().Format(time.RFC3339)
	if got := d.booted.Get(); got != want {
		t.Fatalf("last boot = %q, want %q", got, want)
	}

	d.boot(btime(t, at+1))
	if got := d.booted.Get(); got != want {
		t.Errorf("a one second wobble moved it to %q", got)
	}

	d.boot(btime(t, at+3600))
	if got, moved := d.booted.Get(), time.Unix(at+3600, 0).UTC().Format(time.RFC3339); got != moved {
		t.Errorf("a clock set an hour on left it at %q, want %q", got, moved)
	}
}

// With no /proc/stat to read there is nothing to say, and the sensor stays unknown.
func TestBootWithNothingToReadStaysUnknown(t *testing.T) {
	d := &Diag{booted: &esphome.TextSensor{}}
	d.boot(metrics.Reader{Root: t.TempDir()})
	if got := d.booted.Get(); got != "" {
		t.Errorf("last boot = %q with nothing to read", got)
	}
}
