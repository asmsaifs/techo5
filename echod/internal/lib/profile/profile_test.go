package profile

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestDuration(t *testing.T) {
	cases := map[string]time.Duration{
		"":       defaultFor,
		"\n":     defaultFor,
		"abc":    defaultFor,
		"0":      defaultFor,
		"-5":     defaultFor,
		"45\n":   45 * time.Second,
		" 10 ":   10 * time.Second,
		"100000": maxFor,
	}
	for in, want := range cases {
		if got := duration(in); got != want {
			t.Errorf("duration(%q) = %v, want %v", in, got, want)
		}
	}
}

func TestWatchWritesOneProfile(t *testing.T) {
	dir := t.TempDir()
	askPath, outDir, pollEvery = filepath.Join(dir, "profile"), dir, 10*time.Millisecond
	if err := os.WriteFile(askPath, []byte("1"), 0o644); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	done := make(chan struct{})
	go func() { Watch(ctx); close(done) }()

	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		m, _ := filepath.Glob(filepath.Join(dir, "cpu-*.pprof"))
		if len(m) == 1 {
			if fi, err := os.Stat(m[0]); err == nil && fi.Size() > 0 {
				if _, err := os.Stat(askPath); !os.IsNotExist(err) {
					t.Errorf("request file still there: %v", err)
				}
				cancel()
				<-done
				return
			}
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("no profile written")
}
