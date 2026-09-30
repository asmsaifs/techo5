// Package profile writes a CPU profile of the daemon on request, for finding where its time goes on a
// device. Nothing listens for it: a profile is asked for by creating a file under /run, which only root
// on the device can do, so this adds no way in.
//
//	echo 60 > /run/techo5/profile    # seconds, 30 if the file is empty; the file is removed
//
// The profile lands in /data/techo5-linux/cpu-<time>.pprof, for `go tool pprof` on another machine.
package profile

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"runtime/pprof"
	"strconv"
	"strings"
	"time"
)

var (
	askPath   = "/run/techo5/profile"
	outDir    = "/data/techo5-linux"
	pollEvery = 5 * time.Second
)

const (
	defaultFor = 30 * time.Second
	maxFor     = 5 * time.Minute
)

// Watch answers each request until ctx ends. One profile runs at a time; a request made during one
// waits for the next look.
func Watch(ctx context.Context) {
	t := time.NewTicker(pollEvery)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
		d, ok := asked()
		if !ok {
			continue
		}
		path, err := record(ctx, d)
		if err != nil {
			slog.Warn("profile", "err", err)
			continue
		}
		slog.Info("profile written", "path", path, "for", d)
	}
}

// asked reports whether a profile was asked for, and for how long, removing the request.
func asked() (time.Duration, bool) {
	b, err := os.ReadFile(askPath)
	if err != nil {
		return 0, false
	}
	os.Remove(askPath)
	return duration(string(b)), true
}

// duration reads the request's seconds: empty or unreadable is the default, and it is held to maxFor.
func duration(s string) time.Duration {
	n, err := strconv.Atoi(strings.TrimSpace(s))
	if err != nil || n <= 0 {
		return defaultFor
	}
	d := time.Duration(n) * time.Second
	if d > maxFor {
		d = maxFor
	}
	return d
}

func record(ctx context.Context, d time.Duration) (string, error) {
	path := filepath.Join(outDir, "cpu-"+time.Now().Format("20060102-150405")+".pprof")
	f, err := os.Create(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	if err := pprof.StartCPUProfile(f); err != nil {
		os.Remove(path)
		return "", fmt.Errorf("start: %w", err)
	}
	slog.Info("profile started", "for", d)
	select {
	case <-ctx.Done():
	case <-time.After(d):
	}
	pprof.StopCPUProfile()
	return path, nil
}
