package xiaozhi

import (
	"os"
	"strconv"
	"strings"
	"time"
)

// cpuMeter reads this process's share of one core between samples, from /proc/self/stat. On a
// host without /proc it reads zero, which is why the tests do not depend on it.
type cpuMeter struct {
	at    time.Time
	ticks float64
}

func newCPUMeter() *cpuMeter {
	m := &cpuMeter{}
	m.at, m.ticks = time.Now(), procTicks()
	return m
}

// sample returns the percentage of one core used since the last sample.
func (m *cpuMeter) sample() float64 {
	now, ticks := time.Now(), procTicks()
	dt := now.Sub(m.at).Seconds()
	used := (ticks - m.ticks) / 100 // USER_HZ is 100 on Linux
	m.at, m.ticks = now, ticks
	if dt <= 0 {
		return 0
	}
	return float64(int(used/dt*1000)) / 10
}

// procTicks is utime+stime in clock ticks.
func procTicks() float64 {
	b, err := os.ReadFile("/proc/self/stat")
	if err != nil {
		return 0
	}
	// The comm field is parenthesised and may hold spaces, so count from the last ")".
	s := string(b)
	f := strings.Fields(s[strings.LastIndexByte(s, ')')+1:])
	if len(f) < 13 {
		return 0
	}
	u, _ := strconv.ParseFloat(f[11], 64)
	k, _ := strconv.ParseFloat(f[12], 64)
	return u + k
}
