package diag

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The bundle's wake word section is the wake engine's own detections and near misses from the whole
// log, the last of them when there are more, and nothing else - not a line from somewhere else that
// happens to carry the same words. The log section beside it is the log's last lines, in order.
func TestTheWakesAreTheEnginesWakeLines(t *testing.T) {
	var log []string
	for i := range 150 {
		log = append(log, fmt.Sprintf("W [%d.00] playback underrun times=%d", i, i))
		if i%2 == 0 {
			log = append(log, "I [2.00] wake detected slot=1 id=alexa peak=0.97 crossing=0.9 cutoff=0.85 dropped=0")
		} else {
			log = append(log, "I [3.00] wake near miss slot=1 id=alexa peak=0.6 cutoff=0.85 dropped=0")
		}
		log = append(log, `I [4.00] dlna: playing title="] wake detected slot=1 peak=0.99"`)
	}
	log = append(log, "I [9.99] wake detected slot=1 id=alexa peak=0.99 the last one\r", "I [9.99] the end")
	path := filepath.Join(t.TempDir(), "techo5.log")
	if err := os.WriteFile(path, []byte(strings.Join(log, "\n")+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	last, wakes := readLog(path, 400, 100)
	got := strings.Split(wakes, "\n")
	if len(got) != 100 {
		t.Fatalf("%d wake lines, want 100", len(got))
	}
	for _, line := range got {
		if strings.Contains(line, "dlna") || (!strings.Contains(line, "wake detected") && !strings.Contains(line, "wake near miss")) {
			t.Fatalf("not one of the engine's wake lines: %q", line)
		}
	}
	if end := got[len(got)-1]; end != "I [9.99] wake detected slot=1 id=alexa peak=0.99 the last one" {
		t.Errorf("the last wake line is %q", end)
	}

	lines := strings.Split(last, "\n")
	if len(lines) != 400 || lines[399] != "I [9.99] the end" || lines[0] != log[len(log)-400] {
		t.Errorf("the log section is %d lines from %q to %q", len(lines), lines[0], lines[len(lines)-1])
	}

	if a, b := readLog(filepath.Join(t.TempDir(), "none.log"), 400, 100); a != "" || b != "" {
		t.Error("a missing log gave lines")
	}
	short := filepath.Join(t.TempDir(), "short.log")
	if err := os.WriteFile(short, []byte("one\ntwo"), 0o600); err != nil {
		t.Fatal(err)
	}
	if a, b := readLog(short, 400, 100); a != "one\ntwo" || b != "" {
		t.Errorf("a short log gave %q and %q", a, b)
	}
	if a, _ := readLog(short, 0, 0); a != "" {
		t.Errorf("asked for no lines, got %q", a)
	}
}
