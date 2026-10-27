//go:build !dot

package dashboard

import (
	"crypto/rand"
	"net"
	"strings"
	"testing"

	"github.com/flynn/noise"
)

// answer is a dashcast's side of the handshake on one connection, with key.
func answer(t *testing.T, c net.Conn, key string) {
	defer c.Close()
	hs, err := noise.NewHandshakeState(noise.Config{
		CipherSuite: suite(), Random: rand.Reader, Pattern: noise.HandshakeNN,
		Prologue: []byte(prologue), PresharedKey: psk(key), PresharedKeyPlacement: 0,
	})
	if err != nil {
		t.Error(err)
		return
	}
	first, err := readFrame(c)
	if err != nil {
		return
	}
	if _, _, _, err := hs.ReadMessage(nil, first); err != nil {
		return // the wrong key: a dashcast closes the connection
	}
	reply, _, _, err := hs.WriteMessage(nil, nil)
	if err == nil {
		_ = writeFrame(c, reply)
	}
}

// Check tells a server that isn't there, a key it doesn't take and one that works apart.
func TestCheckSaysWhatIsWrong(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			go answer(t, c, "right key")
		}
	}()
	addr := ln.Addr().String()
	if said, ok := Check(addr, "right key"); !ok || !strings.HasPrefix(said, "Connected") {
		t.Errorf("the right key: %q, %v", said, ok)
	}
	if said, ok := Check(addr, "wrong key"); ok || !strings.Contains(said, "didn't take the key") {
		t.Errorf("the wrong key: %q, %v", said, ok)
	}
	gone, _ := net.Listen("tcp", "127.0.0.1:0")
	nowhere := gone.Addr().String()
	gone.Close()
	if said, ok := Check(nowhere, "right key"); ok || !strings.HasPrefix(said, "Can't reach") {
		t.Errorf("no server: %q, %v", said, ok)
	}
	if _, ok := Check("", "right key"); ok {
		t.Error("no address said it works")
	}
}
