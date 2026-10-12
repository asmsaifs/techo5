package wifi

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// techo5-net (tools/linux/rootfs/usr/local/sbin) run under a POSIX shell with everything it would touch
// on a device replaced: ip, arping, ping, udhcpc, wpa_cli, pidof, killall, flock and sleep are stubs
// that keep the interface, its route, the DHCP client and the network in files, and TECHO5_NET_ROOT and
// TECHO5_NETWORK_CONF keep its own files in a temporary directory. The image's shell is busybox ash;
// this runs it under sh (dash on most Linux machines), and under busybox too where there is one.

const netScript = "../../../../tools/linux/rootfs/usr/local/sbin/techo5-net"

// Where the stubs keep the device's state, all under $STUB:
//
//	addrs        the interface's IPv4 addresses, one address/prefix a line
//	route        the default route, as ip prints it
//	answers      gateways that answer a ping or an ARP request: "G" from anywhere, "G from A" from A only
//	taken        addresses another device answers for (arping -D)
//	refuse       there: ip addr add fails
//	lease        the address/prefix DHCP hands out, lease-gw its gateway
//	ssid, state  what wpa_cli status says (state COMPLETED when missing)
//	udhcpc-on    there while the DHCP client runs
//	calls        every ip, udhcpc and killall call, a line each
var netStubs = map[string]string{
	"ip": `echo "ip $*" >> "$STUB/calls"
A="$STUB/addrs"; R="$STUB/route"
case "$*" in
"link set "*) ;;
"-4 addr show dev wlan0")
	[ -s "$A" ] && while read -r a; do echo "    inet $a brd 0.0.0.0 scope global wlan0"; done < "$A" ;;
"-4 addr flush dev wlan0") : > "$A"; rm -f "$R" ;;
"addr add "*) [ -e "$STUB/refuse" ] && exit 2; echo "$3" >> "$A" ;;
"route replace default via "*) echo "default via $5 dev wlan0" > "$R" ;;
"route show default") cat "$R" 2>/dev/null ;;
*) echo "ip stub: $*" >&2; exit 1 ;;
esac
exit 0`,
	"ping": `for gw; do :; done
src=$(head -1 "$STUB/addrs" 2>/dev/null | cut -d/ -f1)
grep -qx -e "$gw" -e "$gw from $src" "$STUB/answers" 2>/dev/null`,
	"arping": `for gw; do :; done
case " $* " in
*" -D "*) ! grep -qx "$gw" "$STUB/taken" 2>/dev/null; exit ;;
esac
src=$(head -1 "$STUB/addrs" 2>/dev/null | cut -d/ -f1)
grep -qx -e "$gw" -e "$gw from $src" "$STUB/answers" 2>/dev/null`,
	"udhcpc": `echo "udhcpc $*" >> "$STUB/calls"
touch "$STUB/udhcpc-on"
if [ -s "$STUB/lease" ]; then
	cat "$STUB/lease" > "$STUB/addrs"
	echo "default via $(cat "$STUB/lease-gw") dev wlan0" > "$STUB/route"
	printf 'search home.arpa\nnameserver %s\n' "$(cat "$STUB/lease-gw")" > "$TECHO5_NET_ROOT/run/resolv.conf"
fi`,
	"pidof":   `[ "$1" = udhcpc ] && [ -e "$STUB/udhcpc-on" ]`,
	"killall": `echo "killall $*" >> "$STUB/calls"; rm -f "$STUB/udhcpc-on"; : > "$STUB/addrs"; rm -f "$STUB/route"`,
	"wpa_cli": `echo "wpa_state=$(cat "$STUB/state" 2>/dev/null || echo COMPLETED)"
[ -e "$STUB/ssid" ] && echo "ssid=$(cat "$STUB/ssid")"
exit 0`,
	"flock": `exit 0`,
	"sleep": `exit 0`,
}

// netShells is the shells to run the script under: sh, and busybox's where there is one.
func netShells(t *testing.T) [][]string {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("techo5-net runs on the device's Linux; its tests need a POSIX shell and tools")
	}
	if _, err := os.Stat(netScript); err != nil {
		t.Skipf("no techo5-net here: %v", err)
	}
	var shells [][]string
	if sh, err := exec.LookPath("sh"); err == nil {
		shells = append(shells, []string{sh})
	}
	if bb, err := exec.LookPath("busybox"); err == nil {
		if busyboxUsesPath(t, bb) {
			shells = append(shells, []string{bb, "sh"})
		} else {
			t.Logf("%s runs its own applets before PATH (a static build); left out, as the stubs would not be used", bb)
		}
	}
	if len(shells) == 0 {
		t.Skip("no sh to run techo5-net under")
	}
	return shells
}

// busyboxUsesPath is whether busybox's shell runs a command from PATH rather than its own applet of the
// same name. A build that prefers its applets (busybox-static) would run its own ip and arping, not the
// stubs, and the script's tests mean nothing under it.
func busyboxUsesPath(t *testing.T, bb string) bool {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "ip"), []byte("#!/bin/sh\necho stub\n"), 0o755); err != nil {
		return false
	}
	cmd := exec.Command(bb, "sh", "-c", "ip")
	cmd.Env = append(os.Environ(), "PATH="+dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	out, _ := cmd.Output()
	return strings.TrimSpace(string(out)) == "stub"
}

// netBench is one device's worth of state for techo5-net.
type netBench struct {
	t     *testing.T
	shell []string
	dir   string
	stub  string
	root  string
	conf  string
	path  string
}

func newNetBench(t *testing.T, shell []string) *netBench {
	t.Helper()
	dir := t.TempDir()
	b := &netBench{t: t, shell: shell, dir: dir, stub: filepath.Join(dir, "stub"), root: filepath.Join(dir, "root"),
		conf: filepath.Join(dir, "data", "network.conf")}
	bin := filepath.Join(dir, "bin")
	for _, d := range []string{b.stub, bin, filepath.Join(b.root, "run", "techo5"), filepath.Join(b.root, "tmp"), filepath.Dir(b.conf)} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	for name, body := range netStubs {
		if err := os.WriteFile(filepath.Join(bin, name), []byte("#!/bin/sh\n"+body+"\n"), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	b.path = bin + string(os.PathListSeparator) + os.Getenv("PATH")
	return b
}

// run runs techo5-net with args and returns what it printed.
func (b *netBench) run(args ...string) string {
	b.t.Helper()
	cmd := exec.Command(b.shell[0], append(append(b.shell[1:], netScript), args...)...)
	cmd.Env = append(os.Environ(), "PATH="+b.path, "STUB="+b.stub, "TECHO5_NET_ROOT="+b.root,
		"TECHO5_NETWORK_CONF="+b.conf, "UDHCPC_SCRIPT=/nonexistent/udhcpc.sh")
	out, err := cmd.CombinedOutput()
	if err != nil {
		b.t.Fatalf("techo5-net %s: %v\n%s", strings.Join(args, " "), err, out)
	}
	return string(out)
}

func (b *netBench) set(name, content string) {
	b.t.Helper()
	if err := os.WriteFile(b.file(name), []byte(content), 0o644); err != nil {
		b.t.Fatal(err)
	}
}

func (b *netBench) file(name string) string {
	switch {
	case name == "conf":
		return b.conf
	case name == "new":
		return b.conf + ".new"
	case strings.HasPrefix(name, "run/"):
		return filepath.Join(b.root, name)
	}
	return filepath.Join(b.stub, name)
}

func (b *netBench) read(name string) string {
	c, _ := os.ReadFile(b.file(name))
	return strings.TrimSpace(string(c))
}

func (b *netBench) exists(name string) bool {
	_, err := os.Stat(b.file(name))
	return err == nil
}

func (b *netBench) calls() string { return b.read("calls") }

// The setting the tests use: 192.168.1.50/24 via 192.168.1.1 on the network named Home.
const (
	homeHex   = "486f6d65"
	fixedConf = "MODE=static\nADDRESS=192.168.1.50\nPREFIX=24\nGATEWAY=192.168.1.1\nDNS=\"\"\nSSID=" + homeHex + "\n"
)

// onDHCP is a device on its lease, 192.168.1.77, on Home, with the gateway answering.
func (b *netBench) onDHCP() {
	b.set("lease", "192.168.1.77/24\n")
	b.set("lease-gw", "192.168.1.1\n")
	b.set("addrs", "192.168.1.77/24\n")
	b.set("route", "default via 192.168.1.1 dev wlan0\n")
	b.set("udhcpc-on", "")
	b.set("ssid", "Home\n")
	b.set("answers", "192.168.1.1\n")
	b.set("run/resolv.conf", "search home.arpa\nnameserver 192.168.1.1\n")
	b.set("run/techo5/network-applied", "dhcp\n")
}

// onFixed is a device on its fixed address, kept, on Home, with the gateway answering and the two
// minutes after joining over.
func (b *netBench) onFixed() {
	b.onDHCP()
	b.set("conf", fixedConf)
	b.set("addrs", "192.168.1.50/24\n")
	_ = os.Remove(b.file("udhcpc-on"))
	b.set("run/techo5/network-applied", "static\n")
	b.set("run/techo5/network-grace", "0\n")
}

func TestTechoNetScript(t *testing.T) {
	for _, shell := range netShells(t) {
		t.Run(filepath.Base(shell[0]), func(t *testing.T) { netScriptCases(t, shell) })
	}
}

func netScriptCases(t *testing.T, shell []string) {
	t.Run("a trial that works is kept", func(t *testing.T) {
		b := newNetBench(t, shell)
		b.onDHCP()
		b.set("new", fixedConf)
		b.run("try")
		if b.exists("new") || !strings.Contains(b.read("conf"), "ADDRESS=192.168.1.50") {
			t.Errorf("the candidate was not kept: conf %q", b.read("conf"))
		}
		if b.read("addrs") != "192.168.1.50/24" || b.read("route") != "default via 192.168.1.1 dev wlan0" {
			t.Errorf("interface %q, route %q", b.read("addrs"), b.read("route"))
		}
		if !strings.Contains(b.calls(), "ip addr add 192.168.1.50/24 brd + dev wlan0") {
			t.Errorf("the address went on without its broadcast address:\n%s", b.calls())
		}
		if b.exists("udhcpc-on") || b.read("run/techo5/network-applied") != "static" || b.exists("run/techo5/network-fallback") {
			t.Error("the DHCP client still runs, or the fixed address is not recorded as applied, or a fallback is reported")
		}
		// The router's search domain stays, so short names still resolve, and is kept for later boots.
		if got := b.read("run/resolv.conf"); got != "search home.arpa\nnameserver 192.168.1.1" {
			t.Errorf("resolv.conf %q", got)
		}
		if got, _ := os.ReadFile(b.conf + ".search"); strings.TrimSpace(string(got)) != "search home.arpa" {
			t.Errorf("the search domain was not kept: %q", got)
		}
		if runtime.GOOS != "windows" {
			if st, err := os.Stat(b.conf); err == nil && st.Mode().Perm() != 0o600 {
				t.Errorf("network.conf is %o, want 600", st.Mode().Perm())
			}
		}
	})

	t.Run("a trial of an address another device has is refused before anything changes", func(t *testing.T) {
		b := newNetBench(t, shell)
		b.onDHCP()
		b.set("new", fixedConf)
		b.set("taken", "192.168.1.50\n")
		b.run("try")
		if !strings.Contains(b.read("run/techo5/network-fallback"), "already in use") {
			t.Errorf("fallback %q", b.read("run/techo5/network-fallback"))
		}
		if b.exists("new") || b.exists("conf") {
			t.Error("the refused candidate became the setting, or stayed as a candidate")
		}
		if strings.Contains(b.calls(), "addr add") || strings.Contains(b.calls(), "flush") || strings.Contains(b.calls(), "killall") || !b.exists("udhcpc-on") || b.read("addrs") != "192.168.1.77/24" {
			t.Errorf("the device left its lease for a refused address:\n%s", b.calls())
		}
	})

	t.Run("a trial whose gateway does not answer goes back to the setting in use", func(t *testing.T) {
		b := newNetBench(t, shell)
		b.onDHCP()
		b.set("new", fixedConf)
		b.set("answers", "192.168.1.1 from 192.168.1.77\n") // the router answers the lease only
		b.run("try")
		if !strings.Contains(b.read("run/techo5/network-fallback"), "did not answer") {
			t.Errorf("fallback %q", b.read("run/techo5/network-fallback"))
		}
		if b.exists("conf") || !b.exists("udhcpc-on") || b.read("addrs") != "192.168.1.77/24" {
			t.Errorf("not back on DHCP: conf there %v, addrs %q", b.exists("conf"), b.read("addrs"))
		}
	})

	t.Run("a refused address goes back to the fixed address in use", func(t *testing.T) {
		b := newNetBench(t, shell)
		b.onFixed()
		b.set("new", strings.Replace(fixedConf, "192.168.1.50", "192.168.1.60", 1))
		b.set("refuse", "")
		b.run("try")
		if !strings.Contains(b.read("run/techo5/network-fallback"), "was refused") {
			t.Errorf("fallback %q", b.read("run/techo5/network-fallback"))
		}
		_ = os.Remove(b.file("refuse"))
		b.run("up")
		if b.read("addrs") != "192.168.1.50/24" || !strings.Contains(b.read("conf"), "192.168.1.50") {
			t.Errorf("addrs %q, conf %q", b.read("addrs"), b.read("conf"))
		}
	})

	t.Run("a DHCP candidate needs no trial", func(t *testing.T) {
		b := newNetBench(t, shell)
		b.onFixed()
		b.set("run/techo5/network-dhcp-session", "x\n")
		b.set("new", "MODE=dhcp\n")
		b.run("try")
		if b.read("conf") != "MODE=dhcp" || b.exists("new") || b.exists("run/techo5/network-dhcp-session") {
			t.Errorf("conf %q", b.read("conf"))
		}
		if !b.exists("udhcpc-on") || b.read("addrs") != "192.168.1.77/24" || b.read("run/techo5/network-applied") != "dhcp" {
			t.Errorf("not on DHCP: addrs %q", b.read("addrs"))
		}
	})

	t.Run("five silent minutes give way to DHCP", func(t *testing.T) {
		b := newNetBench(t, shell)
		b.onFixed()
		b.set("answers", "")
		for i := 1; i <= 4; i++ {
			b.run("check")
			if b.read("run/techo5/network-applied") != "static" {
				t.Fatalf("gave way after %d minutes", i)
			}
		}
		b.run("check")
		if !strings.Contains(b.read("run/techo5/network-dhcp-session"), "five minutes") || !b.exists("udhcpc-on") {
			t.Errorf("still on the fixed address after five silent minutes: %q", b.read("addrs"))
		}
		if !strings.Contains(b.read("conf"), "192.168.1.50") {
			t.Error("the setting itself was changed")
		}
	})

	t.Run("no misses counted off the network or in the two minutes after joining", func(t *testing.T) {
		b := newNetBench(t, shell)
		b.onFixed()
		b.set("answers", "")
		b.set("state", "SCANNING\n")
		for i := 0; i < 10; i++ {
			b.run("check")
		}
		if b.exists("run/techo5/network-gateway-misses") || b.read("run/techo5/network-applied") != "static" {
			t.Fatal("misses counted while not joined")
		}
		_ = os.Remove(b.file("state"))
		for i := 1; i <= 6; i++ {
			b.run("check")
			if b.read("run/techo5/network-applied") != "static" {
				t.Fatalf("gave way %d minutes after joining", i)
			}
		}
		b.run("check")
		if b.read("run/techo5/network-applied") != "dhcp" {
			t.Error("did not give way seven silent minutes after joining")
		}
	})

	t.Run("a garbled miss counter starts over", func(t *testing.T) {
		b := newNetBench(t, shell)
		b.onFixed()
		b.set("answers", "")
		b.set("run/techo5/network-gateway-misses", "3x\n")
		b.run("check")
		if got := b.read("run/techo5/network-gateway-misses"); got != "1" {
			t.Errorf("counter %q", got)
		}
		b.set("run/techo5/network-grace", "junk\n")
		b.run("check")
		if got := b.read("run/techo5/network-grace"); got != "1" {
			t.Errorf("grace %q (a garbled one counts as just joined)", got)
		}
	})

	t.Run("on another network the fixed address waits, and comes back with the network", func(t *testing.T) {
		b := newNetBench(t, shell)
		b.onDHCP()
		b.set("conf", fixedConf)
		b.set("ssid", "Elsewhere\n")
		b.run("up")
		if !strings.Contains(b.read("run/techo5/network-dhcp-session"), "another network") || !b.exists("udhcpc-on") {
			t.Fatalf("session %q", b.read("run/techo5/network-dhcp-session"))
		}
		b.run("check")
		if b.read("addrs") != "192.168.1.77/24" {
			t.Fatal("the fixed address went on on another network")
		}
		// Back on Home: a Wi-Fi join (renew) puts it straight back on, as at boot.
		b.set("ssid", "Home\n")
		b.run("renew")
		if b.exists("run/techo5/network-dhcp-session") || b.read("addrs") != "192.168.1.50/24" {
			t.Errorf("not back on the fixed address: %q", b.read("addrs"))
		}
	})

	t.Run("after a silent gateway the fixed address comes back once the gateway answers", func(t *testing.T) {
		b := newNetBench(t, shell)
		b.onDHCP()
		b.set("conf", fixedConf)
		b.set("run/techo5/network-dhcp-session", "the gateway 192.168.1.1 did not answer for five minutes\n")
		b.set("answers", "")
		b.run("check")
		if b.read("addrs") != "192.168.1.77/24" {
			t.Fatal("the fixed address went back on with the gateway still silent")
		}
		b.set("answers", "192.168.1.1\n")
		b.run("check")
		if b.exists("run/techo5/network-dhcp-session") || b.read("addrs") != "192.168.1.50/24" ||
			b.read("run/techo5/network-applied") != "static" {
			t.Errorf("not back: addrs %q", b.read("addrs"))
		}
	})

	t.Run("a way back that does not hold is tried again only half an hour later", func(t *testing.T) {
		b := newNetBench(t, shell)
		b.onDHCP()
		b.set("conf", fixedConf)
		b.set("run/techo5/network-dhcp-session", "the gateway 192.168.1.1 did not answer for five minutes\n")
		b.set("answers", "192.168.1.1 from 192.168.1.77\n") // it answers the lease, not the fixed address
		b.run("check")
		if !b.exists("run/techo5/network-dhcp-session") || b.read("addrs") != "192.168.1.77/24" {
			t.Fatalf("not back on DHCP: %q", b.read("addrs"))
		}
		before := strings.Count(b.calls(), "addr add")
		for i := 0; i < 29; i++ {
			b.run("check")
		}
		if n := strings.Count(b.calls(), "addr add"); n != before {
			t.Errorf("tried %d more times within the half hour", n-before)
		}
		b.set("answers", "192.168.1.1\n")
		b.run("check")
		b.run("check")
		if b.read("addrs") != "192.168.1.50/24" {
			t.Errorf("not tried again after the half hour: %q", b.read("addrs"))
		}
	})

	t.Run("a fixed address another device has keeps DHCP, and says so once", func(t *testing.T) {
		b := newNetBench(t, shell)
		b.onDHCP()
		b.set("conf", fixedConf)
		b.set("run/techo5/network-dhcp-session", "the gateway 192.168.1.1 did not answer for five minutes\n")
		b.set("answers", "192.168.1.1\n")
		b.set("taken", "192.168.1.50\n")
		b.run("check")
		b.run("check")
		if b.read("addrs") != "192.168.1.77/24" {
			t.Fatalf("the fixed address went on while another device had it: %q", b.read("addrs"))
		}
		if s := b.read("run/techo5/network-dhcp-session"); !strings.Contains(s, "another device answers for the fixed address 192.168.1.50") {
			t.Errorf("session %q does not say the address is in use", s)
		}
		if n := strings.Count(b.read("run/techo5/network.log"), "in use by another device"); n != 1 {
			t.Errorf("logged %d times that the address is in use, want once", n)
		}
		b.set("taken", "")
		b.run("check")
		if b.exists("run/techo5/network-dhcp-session") || b.read("addrs") != "192.168.1.50/24" {
			t.Errorf("not back on the fixed address once it was free: %q", b.read("addrs"))
		}
	})

	t.Run("a fixed address shares the interface with nothing", func(t *testing.T) {
		b := newNetBench(t, shell)
		b.onFixed()
		b.set("addrs", "192.168.1.50/24\n10.0.0.5/24\n")
		b.run("up")
		if b.read("addrs") != "192.168.1.50/24" {
			t.Errorf("addrs %q", b.read("addrs"))
		}
	})

	t.Run("a missing route is put back without taking the address down", func(t *testing.T) {
		b := newNetBench(t, shell)
		b.onFixed()
		_ = os.Remove(b.file("route"))
		b.run("renew")
		if strings.Contains(b.calls(), "flush") || strings.Contains(b.calls(), "addr add") {
			t.Errorf("the address was taken down:\n%s", b.calls())
		}
		if b.read("route") != "default via 192.168.1.1 dev wlan0" {
			t.Errorf("route %q", b.read("route"))
		}
	})

	t.Run("a fixed address on with DHCP set gives way", func(t *testing.T) {
		b := newNetBench(t, shell)
		b.onFixed()
		b.set("conf", "MODE=dhcp\n")
		b.run("check")
		if !b.exists("udhcpc-on") || b.read("run/techo5/network-applied") != "dhcp" {
			t.Error("still on the fixed address")
		}
	})
}

// The daemon and techo5-net read a setting file the same way: what one takes as a fixed address the
// other does too, as the same address, and what one reads as DHCP so does the other. A hand edit the
// two read differently would have the setup page show one setting while the interface has another.
func TestTheDaemonAndTechoNetReadASettingAlike(t *testing.T) {
	shells := netShells(t)
	dir := t.TempDir()
	prev := addressFile
	addressFile = filepath.Join(dir, "network.conf")
	t.Cleanup(func() { addressFile = prev })

	// What the daemon writes reads back in the script as written: quoted name servers, the name in hex.
	a, err := ParseAddress("192.168.1.50/24", "192.168.1.1", "1.1.1.1, 9.9.9.9")
	if err != nil {
		t.Fatal(err)
	}
	if err := writeCandidate(a, "4d79204e657427732022576946692220"); err != nil {
		t.Fatal(err)
	}
	cand, _ := os.ReadFile(candidate())

	files := map[string]string{
		"written by the daemon":   string(cand),
		"plain":                   fixedConf,
		"edited on Windows":       strings.ReplaceAll(fixedConf, "\n", "\r\n"),
		"indented":                "  MODE=static\n\tADDRESS=192.168.1.50  \nPREFIX=24\nGATEWAY=192.168.1.1\n",
		"quoted":                  "MODE=\"static\"\nADDRESS=\"192.168.1.50\"\nPREFIX=\"24\"\nGATEWAY=\"192.168.1.1\"\n",
		"name servers with tabs":  "MODE=static\nADDRESS=192.168.1.50\nPREFIX=24\nGATEWAY=192.168.1.1\nDNS=\"1.1.1.1\t9.9.9.9\"\n",
		"name servers by commas":  "MODE=static\nADDRESS=192.168.1.50\nPREFIX=24\nGATEWAY=192.168.1.1\nDNS=1.1.1.1,9.9.9.9;8.8.8.8\n",
		"a comment":               "# MODE=dhcp\n" + fixedConf,
		"the last line counts":    fixedConf + "ADDRESS=192.168.1.60\n",
		"no newline at the end":   strings.TrimSuffix(fixedConf, "\n"),
		"dhcp":                    "MODE=dhcp\n",
		"empty":                   "",
		"leading zeros":           "MODE=static\nADDRESS=192.168.001.050\nPREFIX=24\nGATEWAY=192.168.1.1\n",
		"a prefix with a zero":    "MODE=static\nADDRESS=192.168.1.50\nPREFIX=024\nGATEWAY=192.168.1.1\n",
		"no prefix":               "MODE=static\nADDRESS=192.168.1.50\nGATEWAY=192.168.1.1\n",
		"a prefix too long":       "MODE=static\nADDRESS=192.168.1.50\nPREFIX=31\nGATEWAY=192.168.1.1\n",
		"a space before":          "MODE=static\nADDRESS= 192.168.1.50\nPREFIX=24\nGATEWAY=192.168.1.1\n",
		"a space in the key":      "MODE=static\nADDRESS =192.168.1.50\nPREFIX=24\nGATEWAY=192.168.1.1\n",
		"the gateway elsewhere":   "MODE=static\nADDRESS=192.168.1.50\nPREFIX=24\nGATEWAY=192.168.2.1\n",
		"the network's address":   "MODE=static\nADDRESS=192.168.1.0\nPREFIX=24\nGATEWAY=192.168.1.1\n",
		"broadcast":               "MODE=static\nADDRESS=192.168.1.50\nPREFIX=24\nGATEWAY=192.168.1.255\n",
		"the same address":        "MODE=static\nADDRESS=192.168.1.1\nPREFIX=24\nGATEWAY=192.168.1.1\n",
		"loopback":                "MODE=static\nADDRESS=127.0.0.5\nPREFIX=8\nGATEWAY=127.0.0.1\n",
		"link-local":              "MODE=static\nADDRESS=169.254.3.4\nPREFIX=16\nGATEWAY=169.254.0.1\n",
		"a /16":                   "MODE=static\nADDRESS=10.0.5.20\nPREFIX=16\nGATEWAY=10.0.0.1\n",
		"four name servers":       "MODE=static\nADDRESS=192.168.1.50\nPREFIX=24\nGATEWAY=192.168.1.1\nDNS=\"1.1.1.1 1.0.0.1 9.9.9.9 8.8.8.8\"\n",
		"a multicast name server": "MODE=static\nADDRESS=192.168.1.50\nPREFIX=24\nGATEWAY=192.168.1.1\nDNS=224.0.0.251\n",
		"a name not in hex":       "MODE=static\nADDRESS=192.168.1.50\nPREFIX=24\nGATEWAY=192.168.1.1\nSSID=Home\n",
		"a name in capitals":      "MODE=static\nADDRESS=192.168.1.50\nPREFIX=24\nGATEWAY=192.168.1.1\nSSID=486F6D65\n",
		"no gateway":              "MODE=static\nADDRESS=192.168.1.50\nPREFIX=24\n",
		"not static":              "MODE=Static\nADDRESS=192.168.1.50\nPREFIX=24\nGATEWAY=192.168.1.1\n",
	}
	for _, shell := range shells {
		b := newNetBench(t, shell)
		for name, content := range files {
			path := filepath.Join(dir, "case.conf")
			if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
				t.Fatal(err)
			}
			got := strings.TrimSpace(b.run("show", path))
			want := "dhcp"
			if a, ssid := readAddress(path); a.Fixed {
				dns := make([]string, len(a.DNS))
				for i, d := range a.DNS {
					dns[i] = d.String()
				}
				want = strings.TrimSpace("static " + a.CIDR() + " via " + a.Gateway.String() + " dns " + strings.Join(dns, " ") + " ssid " + ssid)
			}
			if got != want {
				t.Errorf("%s, %s: the script reads %q, the daemon %q", shell, name, got, want)
			}
		}
		if got := strings.TrimSpace(b.run("show", candidate())); got != "static 192.168.1.50/24 via 192.168.1.1 dns 1.1.1.1 9.9.9.9 ssid 4d79204e657427732022576946692220" {
			t.Errorf("%s: the daemon's candidate reads in the script as %q", shell, got)
		}
	}
}
