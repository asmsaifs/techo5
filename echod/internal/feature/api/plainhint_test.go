package api

import (
	"bytes"
	"io"
	"net"
	"testing"
	"time"
)

// serveOnce accepts one connection through hintListener, reads the hello's frame header as the
// ESPHome library does, and hangs up, as the library does on a plaintext hello.
func serveOnce(t *testing.T) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ln.Close() })
	go func() {
		c, err := hintListener{ln}.Accept()
		if err != nil {
			return
		}
		defer c.Close()
		var header [3]byte
		_, _ = io.ReadFull(c, header[:])
	}()
	return ln.Addr().String()
}

// What a client hears back after sending hello, until the device hangs up.
func reply(t *testing.T, addr string, hello []byte) []byte {
	t.Helper()
	c, err := net.Dial("tcp", addr)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	if _, err := c.Write(hello); err != nil {
		t.Fatal(err)
	}
	_ = c.SetReadDeadline(time.Now().Add(3 * time.Second))
	got, _ := io.ReadAll(c)
	return got
}

// Home Assistant's plaintext hello is told the device needs a key: the reply starts with Noise's
// indicator, which is what aioesphomeapi turns into RequiresEncryptionAPIError.
func TestPlaintextHelloIsToldItNeedsAKey(t *testing.T) {
	got := reply(t, serveOnce(t), []byte{plaintextIndicator, 0x13, 0x01, 0x0a})
	if !bytes.Equal(got, plainReject) {
		t.Fatalf("reply % x, want % x", got, plainReject)
	}
	if got[0] != noiseIndicator || int(got[1])<<8|int(got[2]) != len(got)-3 || got[3] != 0x01 {
		t.Errorf("not a Noise handshake rejection: % x", got)
	}
}

// A Noise hello is left to the library: nothing is added to the conversation.
func TestNoiseHelloIsLeftAlone(t *testing.T) {
	if got := reply(t, serveOnce(t), []byte{noiseIndicator, 0x00, 0x00}); len(got) != 0 {
		t.Errorf("a Noise client was sent % x", got)
	}
}
