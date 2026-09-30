package api

import (
	"os"
	"path/filepath"
	"testing"

	esphome "github.com/ygelfand/go-esphome-device"
)

// Leaving forgets the access and puts a new key in place of the old one: a real key, never zeros
// (which would let any Home Assistant set its own), and one the device can read back.
func TestLeaveForgetsTheAccessAndChangesTheKey(t *testing.T) {
	dir := t.TempDir()
	keyPath, hassPath := filepath.Join(dir, "psk"), filepath.Join(dir, "hass.json")
	var old esphome.PSK
	old[0] = 7
	if err := writePSK(keyPath, old); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(hassPath, []byte(`{"url":"http://ha:8123","token":"t"}`), 0o600); err != nil {
		t.Fatal(err)
	}

	if err := leave(keyPath, hassPath); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(hassPath); !os.IsNotExist(err) {
		t.Errorf("the Home Assistant access is still there (%v)", err)
	}
	k, err := loadPSK(keyPath)
	if err != nil {
		t.Fatal(err)
	}
	if *k == old || *k == (esphome.PSK{}) {
		t.Errorf("the key after leaving is %v", *k)
	}
	if _, err := os.Stat(keyPath + ".new"); !os.IsNotExist(err) {
		t.Error("the key's temporary file was left behind")
	}

	// A device with no access saved leaves just the same.
	if err := leave(keyPath, hassPath); err != nil {
		t.Errorf("leaving twice: %v", err)
	}
}
