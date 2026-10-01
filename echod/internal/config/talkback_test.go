package config

import (
	"os"
	"path/filepath"
	"testing"
)

// The settings hold passwords, so the file is its owner's alone, even where an older build left it
// readable by everyone.
func TestTheSettingsFileIsOwnerOnly(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	if err := os.WriteFile(path, []byte("{}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path+".tmp", nil, 0o644); err != nil {
		t.Fatal(err)
	}
	Use(path)
	pass := "cam-secret"
	if err := Set().TalkBack().Save("admin", &pass, map[string]string{"camera.door": "rtsp://192.168.1.40/x"}); err != nil {
		t.Fatal(err)
	}
	fi, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if m := fi.Mode().Perm(); m != 0o600 {
		t.Errorf("the settings file is %v", m)
	}
}
