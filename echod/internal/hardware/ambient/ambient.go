//go:build !dot

// Package ambient is the Echo Show's light sensor: how bright the room is, for a screen that
// should not glare at night.
//
// The board's sensor sits behind MediaTek's hwmsensor framework rather than IIO: it has to be
// switched on through /sys/class/misc/m_alsps_misc and then reports lux as ABS_X events on the
// input device named m_alsps_input. The driver samples every alsdelay nanoseconds, but the input
// layer drops a value equal to the last, so a reading arrives only when the light changes. (The
// IIO device on this board is the auxadc, which is why metrics.LuxPath finds nothing.)
//
// The 2nd gen Show 8 on TECHO5's mainline kernel is different: its TCS3400 is an IIO device with raw
// color counts and no lux, so it is polled and converted here (iio.go).
package ambient

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"sync"
	"time"

	"github.com/HuskerMinion/techo5/echod/internal/component"
	"github.com/HuskerMinion/techo5/echod/internal/layout"
	"github.com/HuskerMinion/techo5/echod/internal/lib/hook"
	"github.com/HuskerMinion/techo5/echod/internal/lib/input"
	"github.com/HuskerMinion/techo5/echod/internal/service"
)

func init() {
	component.Register(component.Hardware, Get(), component.Order(20),
		component.Supervise(service.Restart(time.Second, 30*time.Second)))
}

const (
	misc       = "/sys/class/misc/m_alsps_misc"
	deviceName = "m_alsps_input"

	// period is how often the driver samples, as alsdelay wants it in nanoseconds. Twice a second
	// follows a light being switched without chasing every flicker. A steady room sends nothing.
	period = 500 * time.Millisecond
)

type Sensor struct {
	// Lux fires with every reading. Listeners must not block: this is the reader's goroutine.
	Lux hook.Hook[float64]

	mu   sync.Mutex
	last float64
	at   time.Time

	dev *input.Device
	iio *tcs3400 // athena on mainline instead of dev
}

var (
	once   sync.Once
	shared *Sensor
)

func Get() *Sensor {
	once.Do(func() { shared = &Sensor{} })
	return shared
}

func (s *Sensor) Name() string { return "light sensor" }

// Current is the latest reading and when it arrived; ok is false before the first one.
func (s *Sensor) Current() (lux float64, at time.Time, ok bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.last, s.at, !s.at.IsZero()
}

// Start switches the sensor on and opens its input node.
func (s *Sensor) Start(context.Context) error {
	if layout.AthenaMainline() {
		return s.startIIO()
	}
	if err := os.WriteFile(misc+"/alsdelay", []byte(fmt.Sprint(period.Nanoseconds())), 0o644); err != nil {
		return fmt.Errorf("ambient: setting the report period: %w", err)
	}
	if err := os.WriteFile(misc+"/alsactive", []byte("1"), 0o644); err != nil {
		return fmt.Errorf("ambient: enabling the sensor: %w", err)
	}
	dev, err := input.Find(deviceName)
	if err != nil {
		return fmt.Errorf("ambient: %w", err)
	}
	s.dev = dev
	if lux, ok := s.seed(dev.Abs); ok {
		slog.Info("light sensor on", "device", dev.Path, "period", period, "lux", lux)
	} else {
		slog.Info("light sensor on", "device", dev.Path, "period", period)
	}
	return nil
}

// seed starts from the value the input device keeps for ABS_X. A steady room sends nothing, so after
// echod restarted the room read as unknown until the light changed, and auto-brightness had nothing
// to go on. The kernel holds the last value the driver reported. Before the driver's first sample
// after a boot that is zero, a dark room, and the first sample of a lit one differs from it, so it is
// sent and replaces it.
func (s *Sensor) seed(abs func(code uint16) (input.AbsInfo, error)) (float64, bool) {
	info, err := abs(0)
	if err != nil {
		slog.Debug("light sensor has no value to start from", "err", err)
		return 0, false
	}
	lux := float64(info.Value)
	s.mu.Lock()
	s.last, s.at = lux, time.Now()
	s.mu.Unlock()
	return lux, true
}

func (s *Sensor) Close() error {
	s.mu.Lock()
	t, dev := s.iio, s.dev
	s.iio, s.dev = nil, nil
	s.mu.Unlock()
	if t != nil {
		return nil
	}
	_ = os.WriteFile(misc+"/alsactive", []byte("0"), 0o644)
	if dev == nil {
		return nil
	}
	return dev.Close()
}

// errNotOpen is Run's answer when Close came first.
var errNotOpen = errors.New("ambient: the sensor is not open")

// Run reads until ctx is canceled. The read blocks in the kernel, so cancellation closes the node
// from the side and lets the read fail. What it reads is taken once, at the start: a Close that comes
// first, or while it runs, leaves it nothing to read rather than a nil sensor.
func (s *Sensor) Run(ctx context.Context) error {
	s.mu.Lock()
	t, dev := s.iio, s.dev
	s.mu.Unlock()
	if t != nil {
		return s.runIIO(ctx, t)
	}
	if dev == nil {
		return errNotOpen
	}
	stop := context.AfterFunc(ctx, func() { _ = dev.Close() })
	defer stop()

	for {
		e, err := dev.Read()
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}
			return fmt.Errorf("ambient: reading %s: %w", dev.Path, err)
		}
		if e.Type != input.EvAbs || e.Code != 0 { // ABS_X carries the lux
			continue
		}
		lux := float64(e.Value)
		s.mu.Lock()
		s.last, s.at = lux, time.Now()
		s.mu.Unlock()
		s.Lux.Emit(lux)
	}
}
