//go:build linux

package btaudio

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"strings"
	"time"

	"golang.org/x/sys/unix"
)

// The kernel's Bluetooth management interface, for Secure Connections (#183). The daemon first ran
// btmgmt for it, as techo5-lib.sh does, but btmgmt run from the daemon printed nothing at all, on two
// Shows, while the same command over SSH answered: the setting was never read, so never applied. The
// three commands it needs are asked of the kernel here instead, on the management socket btmgmt itself
// uses, so nothing depends on how btmgmt behaves with no terminal.
//
// A command is a header (opcode, controller index, length, all little-endian) and its parameters; the
// answer is a Command Complete event (opcode, status, what it returns) or a Command Status event with
// an error. Other events (bluetoothd's own commands, settings changing) come on the same socket and are
// passed over.

const (
	mgmtOpReadInfo      = 0x0004
	mgmtOpSetPowered    = 0x0005
	mgmtOpSetSecureConn = 0x002D

	mgmtEvCmdComplete = 0x0001
	mgmtEvCmdStatus   = 0x0002

	// hciDevNone binds the socket to no controller: management sockets take the index per command.
	hciDevNone = 0xffff
	// mgmtHci0 is hci0's index.
	mgmtHci0 = 0
)

// mgmtSettings are the settings bits by name, in btmgmt's order and with its names, so what is read
// here prints as btmgmt info would and readSC reads it the same.
var mgmtSettings = []string{
	"powered", "connectable", "fast-connectable", "discoverable", "bondable", "link-security", "ssp",
	"br/edr", "hs", "le", "advertising", "secure-conn", "debug-keys", "privacy", "configuration",
	"static-addr", "phy-configuration", "wide-band-speech",
}

func settingNames(bits uint32) string {
	var names []string
	for i, n := range mgmtSettings {
		if bits&(1<<i) != 0 {
			names = append(names, n)
		}
	}
	return strings.Join(names, " ")
}

// runMgmt runs one of the btmgmt commands Secure Connections uses ("info", "power on", "power off",
// "sc on", "sc off") on hci0 through the management socket, within timeout. "info" gives btmgmt info's
// two settings lines; the others give nothing.
func runMgmt(ctx context.Context, timeout time.Duration, args ...string) (string, error) {
	cmd := strings.Join(args, " ")
	var op uint16
	var param []byte
	switch cmd {
	case "info":
		op = mgmtOpReadInfo
	case "power on", "power off":
		op, param = mgmtOpSetPowered, []byte{boolByte(cmd == "power on")}
	case "sc on", "sc off":
		op, param = mgmtOpSetSecureConn, []byte{boolByte(cmd == "sc on")}
	default:
		return "", fmt.Errorf("management: no command %q", cmd)
	}
	deadline := time.Now().Add(timeout)
	if d, ok := ctx.Deadline(); ok && d.Before(deadline) {
		deadline = d
	}
	ret, err := mgmtCommand(ctx, op, mgmtHci0, param, deadline)
	if err != nil || op != mgmtOpReadInfo {
		return "", err
	}
	return infoText(ret)
}

func boolByte(b bool) byte {
	if b {
		return 1
	}
	return 0
}

// infoText is Read Controller Information's answer as btmgmt info prints its settings: address (6),
// version (1), manufacturer (2), supported settings (4), current settings (4), then class and names.
func infoText(ret []byte) (string, error) {
	if len(ret) < 17 {
		return "", fmt.Errorf("management: controller information %d bytes long", len(ret))
	}
	sup, cur := binary.LittleEndian.Uint32(ret[9:]), binary.LittleEndian.Uint32(ret[13:])
	return "hci0:\n\tsupported settings: " + settingNames(sup) + "\n\tcurrent settings: " + settingNames(cur) + "\n", nil
}

// mgmtPacket is a command for the management socket.
func mgmtPacket(op, index uint16, param []byte) []byte {
	b := make([]byte, 6, 6+len(param))
	binary.LittleEndian.PutUint16(b[0:], op)
	binary.LittleEndian.PutUint16(b[2:], index)
	binary.LittleEndian.PutUint16(b[4:], uint16(len(param)))
	return append(b, param...)
}

// mgmtAnswer reads one event from the socket: whether it answers op on index, and if so what the
// command returned or why it failed.
func mgmtAnswer(ev []byte, op, index uint16) (ret []byte, answered bool, err error) {
	if len(ev) < 6+3 || binary.LittleEndian.Uint16(ev[2:]) != index {
		return nil, false, nil
	}
	code, p := binary.LittleEndian.Uint16(ev[0:]), ev[6:]
	if n := int(binary.LittleEndian.Uint16(ev[4:])); n < len(p) {
		p = p[:n]
	}
	if (code != mgmtEvCmdComplete && code != mgmtEvCmdStatus) || len(p) < 3 || binary.LittleEndian.Uint16(p) != op {
		return nil, false, nil
	}
	if status := p[2]; status != 0 {
		return nil, true, fmt.Errorf("management: command 0x%04x refused, status 0x%02x", op, status)
	}
	if code == mgmtEvCmdStatus {
		// Pending: the Command Complete follows.
		return nil, false, nil
	}
	return p[3:], true, nil
}

var errMgmtTimeout = errors.New("management: no answer in time")

// mgmtPollSlice is how long one wait for the answer lasts before ctx is looked at again.
const mgmtPollSlice = 200 * time.Millisecond

// mgmtCommand sends op to the controller at index and waits until deadline for its answer, or until
// ctx ends.
func mgmtCommand(ctx context.Context, op, index uint16, param []byte, deadline time.Time) ([]byte, error) {
	fd, err := mgmtOpen()
	if err != nil {
		return nil, err
	}
	defer unix.Close(fd)
	if _, err := unix.Write(fd, mgmtPacket(op, index, param)); err != nil {
		return nil, fmt.Errorf("management: sending: %w", err)
	}
	buf := make([]byte, 1024)
	for {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		wait := time.Until(deadline)
		if wait <= 0 {
			return nil, errMgmtTimeout
		}
		wait = min(wait, mgmtPollSlice)
		fds := []unix.PollFd{{Fd: int32(fd), Events: unix.POLLIN}}
		if n, err := unix.Poll(fds, int(wait/time.Millisecond)+1); err != nil && err != unix.EINTR {
			return nil, fmt.Errorf("management: waiting: %w", err)
		} else if n <= 0 {
			continue
		}
		n, err := unix.Read(fd, buf)
		if err == unix.EAGAIN || err == unix.EINTR {
			continue
		}
		if err != nil {
			return nil, fmt.Errorf("management: reading: %w", err)
		}
		if ret, ok, err := mgmtAnswer(buf[:n], op, index); ok || err != nil {
			return ret, err
		}
	}
}

// mgmtOpen opens a management socket.
func mgmtOpen() (int, error) {
	fd, err := unix.Socket(unix.AF_BLUETOOTH, unix.SOCK_RAW|unix.SOCK_CLOEXEC|unix.SOCK_NONBLOCK, unix.BTPROTO_HCI)
	if err != nil {
		return -1, fmt.Errorf("management: socket: %w", err)
	}
	if err := unix.Bind(fd, &unix.SockaddrHCI{Dev: hciDevNone, Channel: unix.HCI_CHANNEL_CONTROL}); err != nil {
		unix.Close(fd)
		return -1, fmt.Errorf("management: bind: %w", err)
	}
	return fd, nil
}

// haveMgmt reports whether the kernel offers the management socket (every TECHO5 kernel with
// Bluetooth does); one without it skips the setting with a line in the log.
func haveMgmt() bool {
	fd, err := mgmtOpen()
	if err != nil {
		return false
	}
	unix.Close(fd)
	return true
}
