//go:build dot

package wifi

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

// The Echo Dot never offers a fixed address, whatever techo5-net is in place: its own joins Wi-Fi
// whenever it is run, so it is never driven.
func TestTheDotHasNoFixedAddress(t *testing.T) {
	dir := t.TempDir()
	prev, prevConf := netTool, addressFile
	netTool, addressFile = filepath.Join(dir, "techo5-net"), filepath.Join(dir, "network.conf")
	t.Cleanup(func() { netTool, addressFile = prev, prevConf })
	_ = os.WriteFile(netTool, []byte("#!/bin/sh\n# techo5-net-caps: fixed-address\nexit 1\n"), 0o755)
	if AddressSupported() {
		t.Error("the Dot offers a fixed address")
	}
	a, _ := ParseAddress("192.0.2.50", "192.0.2.1", "")
	if _, err := ChangeAddress(context.Background(), a); err == nil {
		t.Error("ChangeAddress went ahead on the Dot")
	}
}
