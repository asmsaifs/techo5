package boot

import (
	"os"
	"path/filepath"
	"testing"
)

func TestReadableName(t *testing.T) {
	for _, c := range []struct{ in, want string }{
		{"Kitchen\n", "Kitchen\n"},
		{"Küche Echo\n", "Küche Echo\n"},
		{"K\xfcche Echo\n", "Küche Echo\n"},
		{"Caf\xe9", "Café"},
		{"\x8aibenik \x9cuvre", "Šibenik œuvre"},
		{"A\x81B\xff", "A\u0081Bÿ"},
	} {
		if got := readableName([]byte(c.in)); got != c.want {
			t.Errorf("readableName(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

// A name file not in UTF-8 is written back as UTF-8 once, with its permissions, so the next start
// reads it as it is; a file already in UTF-8 is left alone, and one that cannot be written back still
// gives the name.
func TestANameNotInUTF8IsWrittenBack(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "name")
	if err := os.WriteFile(path, []byte("K\xfcche Echo\n"), 0o640); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, 0o640); err != nil {
		t.Fatal(err)
	}
	if got := nameIn(path); got != "Küche Echo" {
		t.Fatalf("read %q", got)
	}
	b, err := os.ReadFile(path)
	if err != nil || string(b) != "Küche Echo\n" {
		t.Fatalf("written back as %q, %v", b, err)
	}
	if info, err := os.Stat(path); err != nil || info.Mode().Perm() != 0o640 {
		t.Fatalf("permissions %v, %v; want 0640", info.Mode().Perm(), err)
	}
	if left, _ := filepath.Glob(filepath.Join(dir, ".name.*")); len(left) != 0 {
		t.Fatalf("left behind %v", left)
	}
	if got := nameIn(path); got != "Küche Echo" {
		t.Fatalf("read back %q", got)
	}
	if got := nameIn(filepath.Join(dir, "absent")); got != "" {
		t.Fatalf("no file read as %q", got)
	}

	// A file that cannot be written back is read all the same.
	if os.Getuid() == 0 {
		return // root writes anywhere
	}
	ro := t.TempDir()
	path = filepath.Join(ro, "name")
	if err := os.WriteFile(path, []byte("Caf\xe9"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(ro, 0o555); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chmod(ro, 0o755) })
	if got := nameIn(path); got != "Café" {
		t.Fatalf("read %q from a file that could not be written back", got)
	}
}
