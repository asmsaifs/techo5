//go:build !dot

package wifi

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

// fakeNet is a stand-in techo5-net: it notes each run in calls and does what its script says.
type fakeNet struct {
	dir   string
	calls string
	addr  string // the interface's address, which the fake's script can change
}

func newFakeNet(t *testing.T, script string) *fakeNet {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("runs a shell script as techo5-net")
	}
	dir := t.TempDir()
	f := &fakeNet{dir: dir, calls: filepath.Join(dir, "calls"), addr: filepath.Join(dir, "addr")}
	tool := filepath.Join(dir, "techo5-net")
	body := "#!/bin/sh\n# techo5-net-caps: fixed-address\necho \"$1\" >> " + f.calls + "\n" + script + "\n"
	if err := os.WriteFile(tool, []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}
	_ = os.WriteFile(f.addr, []byte("192.0.2.61"), 0o644)
	prevTool, prevConf, prevFallback, prevSession := netTool, addressFile, fallbackFile, sessionFile
	prevTimeout, prevAddr, prevPoll := trialTimeout, currentAddress, addressPoll
	netTool = tool
	addressFile = filepath.Join(dir, "network.conf")
	fallbackFile = filepath.Join(dir, "network-fallback")
	sessionFile = filepath.Join(dir, "network-dhcp-session")
	addressPoll = time.Millisecond
	currentAddress = func() string {
		b, _ := os.ReadFile(f.addr)
		return strings.TrimSpace(string(b))
	}
	t.Cleanup(func() {
		netTool, addressFile, fallbackFile, sessionFile = prevTool, prevConf, prevFallback, prevSession
		trialTimeout, currentAddress, addressPoll = prevTimeout, prevAddr, prevPoll
	})
	return f
}

func (f *fakeNet) ran() string {
	b, _ := os.ReadFile(f.calls)
	return strings.TrimSpace(string(b))
}

func fixed50(t *testing.T) Address {
	t.Helper()
	a, err := ParseAddress("192.0.2.50/24", "192.0.2.1", "")
	if err != nil {
		t.Fatal(err)
	}
	return a
}

// A trial that runs past its time is cut short, and the setting in use goes back on (techo5-net up):
// the untried address may be on the interface with the DHCP client stopped.
func TestATrialCutShortPutsTheSettingInUseBack(t *testing.T) {
	f := newFakeNet(t, `[ "$1" = try ] && exec sleep 30`)
	trialTimeout = 300 * time.Millisecond
	start := time.Now()
	moved, err := ChangeAddress(context.Background(), fixed50(t))
	if err == nil || moved {
		t.Fatalf("a trial cut short: moved %v, err %v", moved, err)
	}
	if d := time.Since(start); d > 10*time.Second {
		t.Errorf("took %s", d)
	}
	if got := f.ran(); got != "try\nup" {
		t.Errorf("ran %q, want try then up", got)
	}
	if _, err := os.Stat(candidate()); err == nil {
		t.Error("the candidate of a trial cut short is still there")
	}
	if Trialing() {
		t.Error("still trialing")
	}
}

// A trial that moves the device says so; one that is not kept is no error, and the fallback says why.
func TestATrialSaysWhetherTheDeviceMoved(t *testing.T) {
	f := newFakeNet(t, `[ "$1" = try ] && echo 192.0.2.50 > "$(dirname "$0")/addr"`)
	moved, err := ChangeAddress(context.Background(), fixed50(t))
	if err != nil || !moved {
		t.Fatalf("moved %v, err %v", moved, err)
	}
	if got := f.ran(); got != "try" {
		t.Errorf("ran %q", got)
	}

	f = newFakeNet(t, `[ "$1" = try ] && echo "the gateway 192.0.2.1 did not answer from 192.0.2.50/24" > "$(dirname "$0")/network-fallback"`)
	moved, err = ChangeAddress(context.Background(), fixed50(t))
	if err != nil || moved {
		t.Fatalf("a setting not kept: moved %v, err %v", moved, err)
	}
	if AddressFallback() == "" || f.ran() != "try" {
		t.Errorf("no reason given, or ran %q", f.ran())
	}
}

// The setting in use and in place is not tried again; while DHCP stands in for it, it is.
func TestTheSameSettingIsTriedAgainOnlyWhileDHCPStandsIn(t *testing.T) {
	f := newFakeNet(t, "")
	a := fixed50(t)
	if err := os.WriteFile(addressFile, []byte(fixedConfFor(a)), 0o600); err != nil {
		t.Fatal(err)
	}
	_ = os.WriteFile(f.addr, []byte("192.0.2.50"), 0o644)
	if _, err := ChangeAddress(context.Background(), a); err != nil {
		t.Fatal(err)
	}
	if got := f.ran(); got != "" {
		t.Errorf("the setting in place was tried again: %q", got)
	}
	_ = os.WriteFile(sessionFile, []byte("the gateway 192.0.2.1 did not answer for five minutes"), 0o644)
	_ = os.WriteFile(f.addr, []byte("192.0.2.77"), 0o644)
	if _, err := ChangeAddress(context.Background(), a); err != nil {
		t.Fatal(err)
	}
	if got := f.ran(); got != "try" {
		t.Errorf("the setting DHCP stands in for was not tried again: %q", got)
	}
}

// A techo5-net that is not the one that tries fixed addresses (the Echo Dot's joins Wi-Fi) is never run.
func TestAnotherTechoNetIsNeverRun(t *testing.T) {
	f := newFakeNet(t, "")
	other := filepath.Join(f.dir, "dot-techo5-net")
	_ = os.WriteFile(other, []byte("#!/bin/sh\n# techo5-net [restart]: join Wi-Fi and keep a DHCP lease.\necho \"$1\" >> "+f.calls+"\n"), 0o755)
	netTool = other
	if AddressSupported() {
		t.Error("a techo5-net without the fixed-address line counts as supported")
	}
	if _, err := ChangeAddress(context.Background(), fixed50(t)); err == nil {
		t.Error("ChangeAddress went ahead")
	}
	if got := f.ran(); got != "" {
		t.Errorf("it was run: %q", got)
	}
	netTool = filepath.Join(f.dir, "techo5-net")
	if !AddressSupported() {
		t.Error("the right techo5-net does not count as supported")
	}
}

func fixedConfFor(a Address) string {
	return "MODE=static\nADDRESS=" + a.IP.String() + "\nPREFIX=24\nGATEWAY=" + a.Gateway.String() + "\nDNS=\"\"\nSSID=\n"
}
