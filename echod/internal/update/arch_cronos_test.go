//go:build !dot && !spot

package update

import (
	"strings"
	"testing"
)

// The 2nd gen Show 8 (athena) runs the Show's binary but boots a rootfs of its own. A release's
// plain "arm" rootfs is the other Shows' and must never be offered to an athena or installed there;
// its own is keyed "arm-athena", which the other Shows in turn never take.
func TestAthenaTakesOnlyItsOwnRootfs(t *testing.T) {
	restoreArch, restoreBoard := arch, onAthena
	t.Cleanup(func() { arch, onAthena = restoreArch, restoreBoard })
	arch = "arm"

	rootfs := func(key string) Binary {
		return Binary{URL: "https://example/techo5-rootfs-" + key + ".tar.gz", SHA256: strings.Repeat("d", 64), Size: 60 << 20}
	}
	bin := map[string]Binary{"arm": {URL: "https://example/echod-arm", SHA256: strings.Repeat("b", 64), Size: 22 << 20}}
	showOnly := Manifest{Version: "1.3.0", Binaries: bin, Rootfs: map[string]Binary{"arm": rootfs("arm")}}
	both := Manifest{Version: "1.3.0", Binaries: bin, Rootfs: map[string]Binary{"arm": rootfs("arm"), "arm-athena": rootfs("athena")}}

	onAthena = func() bool { return true }
	if got := rootfsArch(); got != "arm-athena" {
		t.Fatalf("athena's rootfs key is %q, want arm-athena", got)
	}
	if showOnly.serves(true) {
		t.Error("a release with only the other Shows' rootfs was offered to an athena")
	}
	if !both.serves(true) {
		t.Error("a release carrying athena's rootfs was not offered to it")
	}

	onAthena = func() bool { return false }
	if got := rootfsArch(); got != "arm" {
		t.Fatalf("the Show's rootfs key is %q, want arm", got)
	}
	if !showOnly.serves(true) {
		t.Error("a Show 5 on slots was not offered the arm rootfs")
	}
	athenaOnly := Manifest{Version: "1.3.0", Binaries: bin, Rootfs: map[string]Binary{"arm-athena": rootfs("athena")}}
	if athenaOnly.serves(true) {
		t.Error("a release with only athena's rootfs was offered to another Show on slots")
	}
	// Off slots (Android) the binary decides, as before, on either board.
	if !athenaOnly.serves(false) {
		t.Error("an Android Show was not offered the arm binary")
	}
}
