//go:build linux

// Package bluealsa talks to bluez-alsa over D-Bus: which A2DP streams exist, and a raw PCM to write
// into. bluez-alsa does the codec and the Bluetooth side; the daemon hands it 16-bit stereo.
package bluealsa

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/godbus/dbus/v5"

	"github.com/HuskerMinion/techo5/echod/internal/lib/bluez"
	"github.com/HuskerMinion/techo5/echod/internal/lib/hook"
)

const (
	service   = "org.bluealsa"
	root      = dbus.ObjectPath("/org/bluealsa")
	pcmIfc    = "org.bluealsa.PCM1"
	propsIfc  = "org.freedesktop.DBus.Properties"
	objMgrIfc = "org.freedesktop.DBus.ObjectManager"
)

// PCM is one stream bluez-alsa offers.
type PCM struct {
	Path      dbus.ObjectPath
	Device    dbus.ObjectPath // the bluez device it belongs to
	Transport string          // "A2DP-source" is what we play into
	Mode      string          // "sink" from bluez-alsa's side: it takes what we write
	Format    uint16
	Channels  int
	Rate      int
	Codec     string
	Running   bool
	// Volume is bluez-alsa's level for the stream, the left channel in the high byte and the right in
	// the low, 0..127 each (bit 7 is mute). For a phone playing to the device it is the phone's volume
	// slider, which the phone sends over AVRCP rather than scaling its audio; SoftVolume is whether
	// bluez-alsa scales the audio by it.
	Volume     uint16
	SoftVolume bool
}

// VolumeMax is the top of one channel's level in Volume.
const VolumeMax = 127

// Level is the stream's level, 0..VolumeMax: the louder channel's, with mute as 0.
func (p PCM) Level() int {
	l, r := int(p.Volume>>8), int(p.Volume&0xff)
	if l&0x80 != 0 {
		l = 0
	}
	if r&0x80 != 0 {
		r = 0
	}
	return max(l, r)
}

// volumeOf is the Volume for level on both channels.
func volumeOf(level int) uint16 {
	level = max(0, min(level, VolumeMax))
	return uint16(level)<<8 | uint16(level)
}

// Playable reports whether this is an A2DP stream the daemon can write into.
func (p PCM) Playable() bool {
	return strings.HasPrefix(p.Transport, "A2DP") && p.Mode == "sink" && p.Channels > 0 && p.Rate > 0
}

// Receivable reports whether this is an A2DP stream the daemon can read from: a phone or a computer
// playing to the device. bluez-alsa names it "source", since it is where the audio comes from.
func (p PCM) Receivable() bool {
	return strings.HasPrefix(p.Transport, "A2DP") && p.Mode == "source" && p.Channels > 0 && p.Rate > 0
}

// Client watches bluez-alsa.
type Client struct {
	conn *dbus.Conn

	// Changed fires when a PCM comes or goes; listeners must not block.
	Changed hook.Hook[struct{}]

	mu   sync.Mutex
	pcms map[dbus.ObjectPath]*PCM
	stop func()
}

// Open connects; it fails while bluez-alsa is not on the bus yet.
func Open(ctx context.Context) (*Client, error) {
	conn, err := bluez.SystemBus()
	if err != nil {
		return nil, fmt.Errorf("system bus: %w", err)
	}
	c := &Client{conn: conn, pcms: map[dbus.ObjectPath]*PCM{}}
	if err := c.load(); err != nil {
		conn.Close()
		return nil, err
	}
	if err := c.watch(); err != nil {
		conn.Close()
		return nil, err
	}
	return c, nil
}

func (c *Client) Close() error {
	if c.stop != nil {
		c.stop()
	}
	return c.conn.Close()
}

func (c *Client) load() error {
	var objs map[dbus.ObjectPath]map[string]map[string]dbus.Variant
	if err := c.conn.Object(service, root).Call(objMgrIfc+".GetManagedObjects", 0).Store(&objs); err != nil {
		return fmt.Errorf("bluealsa: managed objects: %w", err)
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.pcms = map[dbus.ObjectPath]*PCM{}
	for path, ifcs := range objs {
		if props, ok := ifcs[pcmIfc]; ok {
			c.pcms[path] = pcmFrom(path, props)
		}
	}
	return nil
}

func pcmFrom(path dbus.ObjectPath, props map[string]dbus.Variant) *PCM {
	p := &PCM{Path: path}
	p.update(props)
	return p
}

func (p *PCM) update(props map[string]dbus.Variant) {
	for k, v := range props {
		switch k {
		case "Device":
			p.Device, _ = v.Value().(dbus.ObjectPath)
		case "Transport":
			p.Transport, _ = v.Value().(string)
		case "Mode":
			p.Mode, _ = v.Value().(string)
		case "Format":
			p.Format, _ = v.Value().(uint16)
		case "Channels":
			if b, ok := v.Value().(byte); ok {
				p.Channels = int(b)
			}
		case "Rate", "Sampling":
			if r, ok := v.Value().(uint32); ok {
				p.Rate = int(r)
			}
		case "Codec":
			p.Codec, _ = v.Value().(string)
		case "Running":
			p.Running, _ = v.Value().(bool)
		case "Volume":
			p.Volume, _ = v.Value().(uint16)
		case "SoftVolume":
			p.SoftVolume, _ = v.Value().(bool)
		}
	}
}

func (c *Client) watch() error {
	opts := [][]dbus.MatchOption{
		{dbus.WithMatchSender(service), dbus.WithMatchInterface(objMgrIfc), dbus.WithMatchMember("InterfacesAdded")},
		{dbus.WithMatchSender(service), dbus.WithMatchInterface(objMgrIfc), dbus.WithMatchMember("InterfacesRemoved")},
		{dbus.WithMatchSender(service), dbus.WithMatchInterface(propsIfc), dbus.WithMatchMember("PropertiesChanged"), dbus.WithMatchArg(0, pcmIfc)},
	}
	for _, o := range opts {
		if err := c.conn.AddMatchSignal(o...); err != nil {
			return fmt.Errorf("bluealsa: match: %w", err)
		}
	}
	ch := make(chan *dbus.Signal, 64)
	c.conn.Signal(ch)
	ctx, cancel := context.WithCancel(context.Background())
	c.stop = cancel
	go func() {
		for {
			select {
			case <-ctx.Done():
				c.conn.RemoveSignal(ch)
				return
			case s := <-ch:
				if s == nil {
					return
				}
				c.signal(s)
			}
		}
	}()
	return nil
}

func (c *Client) signal(s *dbus.Signal) {
	changed := false
	c.mu.Lock()
	switch s.Name {
	case objMgrIfc + ".InterfacesAdded":
		if len(s.Body) == 2 {
			path, _ := s.Body[0].(dbus.ObjectPath)
			ifcs, _ := s.Body[1].(map[string]map[string]dbus.Variant)
			if props, ok := ifcs[pcmIfc]; ok {
				c.pcms[path] = pcmFrom(path, props)
				changed = true
			}
		}
	case objMgrIfc + ".InterfacesRemoved":
		if len(s.Body) == 2 {
			path, _ := s.Body[0].(dbus.ObjectPath)
			ifcs, _ := s.Body[1].([]string)
			for _, i := range ifcs {
				if i == pcmIfc {
					delete(c.pcms, path)
					changed = true
				}
			}
		}
	case propsIfc + ".PropertiesChanged":
		if len(s.Body) >= 2 {
			if p, ok := c.pcms[s.Path]; ok {
				props, _ := s.Body[1].(map[string]dbus.Variant)
				p.update(props)
				changed = true
			}
		}
	}
	c.mu.Unlock()
	if changed {
		c.Changed.Emit(struct{}{})
	}
}

// PCMs is what bluez-alsa offers right now.
func (c *Client) PCMs() []PCM {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make([]PCM, 0, len(c.pcms))
	for _, p := range c.pcms {
		out = append(out, *p)
	}
	return out
}

// SetSoftVolume has bluez-alsa scale the stream's audio by its Volume, or pass it on as it comes.
func (c *Client) SetSoftVolume(p PCM, on bool) error {
	return c.set(p, "SoftVolume", on)
}

// SetLevel sets the stream's level on both channels, 0..VolumeMax. For a phone playing to the device,
// bluez-alsa passes it on to the phone over AVRCP, and the phone's volume slider moves to it.
func (c *Client) SetLevel(p PCM, level int) error {
	return c.set(p, "Volume", volumeOf(level))
}

func (c *Client) set(p PCM, name string, value any) error {
	call := c.conn.Object(service, p.Path).Call(propsIfc+".Set", 0, pcmIfc, name, dbus.MakeVariant(value))
	if call.Err != nil {
		return fmt.Errorf("bluealsa: set %s: %w", name, call.Err)
	}
	return nil
}

// Stream is an open PCM: write interleaved S16_LE frames at the PCM's rate and channel count.
type Stream struct {
	PCM
	fd   int      // write side (Open)
	in   *os.File // read side (OpenRead)
	ctrl *os.File
}

// Open takes the PCM for writing. bluez-alsa starts the Bluetooth stream when data arrives.
func (c *Client) Open(p PCM) (*Stream, error) {
	var pcmFD, ctrlFD dbus.UnixFD
	if err := c.conn.Object(service, p.Path).Call(pcmIfc+".Open", 0).Store(&pcmFD, &ctrlFD); err != nil {
		return nil, fmt.Errorf("bluealsa: open: %w", err)
	}
	// The PCM side is written without blocking: the speaker loop paces itself on the codec and must
	// not wait on a Bluetooth buffer. A full pipe answers EAGAIN and the caller drops the period.
	_ = syscall.SetNonblock(int(pcmFD), true)
	return &Stream{
		PCM:  p,
		fd:   int(pcmFD),
		ctrl: os.NewFile(uintptr(ctrlFD), "bluealsa-ctrl"),
	}, nil
}

// OpenRead takes a Receivable PCM for reading: interleaved S16_LE at the PCM's rate and channel
// count, as the remote sends it. Reads wait while the remote is quiet, go through Go's poller so a
// deadline or Close ends a waiting read (closing a pipe does not wake a plain blocked read(2)).
func (c *Client) OpenRead(p PCM) (*Stream, error) {
	var pcmFD, ctrlFD dbus.UnixFD
	if err := c.conn.Object(service, p.Path).Call(pcmIfc+".Open", 0).Store(&pcmFD, &ctrlFD); err != nil {
		return nil, fmt.Errorf("bluealsa: open: %w", err)
	}
	// Non-blocking first: os.NewFile hands only a non-blocking descriptor to the poller.
	_ = syscall.SetNonblock(int(pcmFD), true)
	return &Stream{
		PCM:  p,
		fd:   -1,
		in:   os.NewFile(uintptr(pcmFD), "bluealsa-pcm"),
		ctrl: os.NewFile(uintptr(ctrlFD), "bluealsa-ctrl"),
	}, nil
}

// Read takes frames from a stream opened with OpenRead.
func (s *Stream) Read(b []byte) (int, error) {
	if s.in == nil {
		return 0, errors.New("bluealsa: stream is not open for reading")
	}
	return s.in.Read(b)
}

// SetReadDeadline bounds the next reads, for noticing a remote that stopped sending.
func (s *Stream) SetReadDeadline(t time.Time) error {
	if s.in == nil {
		return errors.New("bluealsa: stream is not open for reading")
	}
	return s.in.SetReadDeadline(t)
}

// Write hands over frames. It returns syscall.EAGAIN when bluez-alsa's buffer is full; what was
// not written is dropped.
func (s *Stream) Write(b []byte) (int, error) {
	n := 0
	for n < len(b) {
		m, err := syscall.Write(s.fd, b[n:])
		if err == syscall.EINTR {
			continue
		}
		if err != nil {
			return n, err
		}
		n += m
	}
	return n, nil
}

// Command sends one of bluez-alsa's control words: Drain, Drop, Pause, Resume.
func (s *Stream) Command(word string) error {
	if _, err := s.ctrl.Write([]byte(word)); err != nil {
		return err
	}
	buf := make([]byte, 32)
	n, err := s.ctrl.Read(buf)
	if err != nil {
		return err
	}
	if reply := strings.TrimSpace(string(buf[:n])); reply != "OK" {
		return errors.New("bluealsa: " + reply)
	}
	return nil
}

// Pause stops the Bluetooth transport; what is buffered is dropped so a resume starts fresh.
func (s *Stream) Pause() error {
	if err := s.Command("Pause"); err != nil {
		return err
	}
	return s.Command("Drop")
}

// Resume starts the transport again.
func (s *Stream) Resume() error { return s.Command("Resume") }

// Close ends the stream; bluez-alsa stops the transport once nothing is open.
func (s *Stream) Close() error {
	var err error
	if s.in != nil {
		err = s.in.Close()
	} else {
		err = syscall.Close(s.fd)
	}
	if e := s.ctrl.Close(); err == nil {
		err = e
	}
	return err
}
