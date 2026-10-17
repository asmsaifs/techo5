//go:build !dot && !spot

package layout

import (
	"os"
	"strings"
)

// The Echo Show 5 2nd gen on LineageOS 18.1: nothing of Amazon's is taken over. The daemon is an
// init service of its own (tools/init/techo5.rc) with its state on /data. There are no vendor boot
// hooks to keep current, so AnimationScripts is empty and update.Ensure has nothing to write.
const (
	// On Android the daemon is /system/bin/techo5, which is what tools/init/techo5.rc runs and what
	// the updater replaces in place (remounting / rw, as the Dot does for /system). On the Linux image
	// it is /usr/local/bin/techo5 under busybox init (tools/linux/rootfs); see layout.Dir.
	AndroidDir = "/system/bin"
	BinaryName = "techo5"
	StateDir   = "/data/misc/techo5"

	Service     = AndroidDir + "/" + BinaryName
	ServiceName = "techo5"

	StockLabel = "u:object_r:system_file:s0"

	StartAnimation = ""
	StopAnimation  = ""

	FirewallHook = ""

	// LogTag is the daemon's logcat tag: `adb logcat -s techo5`.
	LogTag = "techo5"

	Manufacturer = "TECHO5"

	// DefaultName is the fallback display name when a device has none recorded.
	DefaultName = "Echo Show"

	// LogPath is where techo5-run sends the daemon's output, and BootLog what the boot script kept
	// of its own run. Both are what a diagnostics bundle reads.
	LogPath = "/data/techo5-linux/techo5.log"
	BootLog = "/run/boot.log"
)

var AnimationScripts = []string{}

// Board and Model say which Echo with a screen this is. One build serves all four: the 1st gen
// Show 5 (checkers, 2019), the 2nd gen Show 5 (cronos, 2021) and the 1st gen Show 8 (crown, 2019)
// run the same kernel commit on the same SoC, and the parts that differ — the speaker codec, the
// mute driver, the camera sensor, the microphone array, the panel — ask at run time.
// techo5-checkers docs/hardware.md has what is known of the 1st gen Show 5; the Show 8 is in
// TECHO5-CROWN crown-port-notes.md, off the repository because the dump beside it is unredacted.
//
// The 2nd gen Show 8 (athena, MT8183) is the odd one out: it runs Amazon's own 32-bit 4.4 kernel,
// not a LineageOS one. Its screen, touch, backlight, buttons and mute are the Show 8's and the 2nd
// gen Show 5's. Its sound card is Amazon's MT8183 one, with the speaker and the microphones on device
// numbers of their own and the playback reference on a second capture device (hardware/mic and
// hardware/speaker ask Athena).
var (
	Board = showBoard(kernelCmdline(), gatingDir, deviceTreeCompatible())
	Model = map[string]string{
		boardCronos:   "Echo Show 5 2nd gen (cronos)",
		boardCheckers: "Echo Show 5 1st gen (checkers)",
		boardCrown:    "Echo Show 8 1st gen (crown)",
		boardAthena:   "Echo Show 8 2nd gen (athena)",
	}[Board]
)

const (
	boardCronos   = "cronos"
	boardCheckers = "checkers"
	boardCrown    = "crown"
	boardAthena   = "athena"

	// gatingDir is Amazon's own mute driver, amazon-gating, which the 1st gen Show 5 and the Show 8
	// both carry; the 2nd gen Show 5 has gpio-privacy instead.
	gatingDir = "/sys/devices/platform/amazon-gating"

	// athenaCompatible is the device tree's root compatible on the 2nd gen Show 8, which Amazon's
	// kernel shows in /proc/device-tree: "amzn,athena-pvt", "amzn,athena-dvt", "amzn,athena" on the
	// unit read 2026-10-14. Its command line names no panel (there is no lcm=), so this is the answer.
	athenaCompatible = "amzn,athena"
)

// Checkers is whether this is the 1st gen Echo Show 5.
func Checkers() bool { return Board == boardCheckers }

// Crown is whether this is the 1st gen Echo Show 8.
func Crown() bool { return Board == boardCrown }

// Athena is whether this is the 2nd gen Echo Show 8.
func Athena() bool { return Board == boardAthena }

// Show8 is whether the screen is the Show 8's: an 800 by 1280 portrait panel in a 1280 by 800
// landscape device, with a FocalTech touch controller behind it. Both generations have it.
func Show8() bool { return Crown() || Athena() }

// showBoard names the board. The bootloader puts it in the kernel command line, inside the panel
// driver it names: lcm=1-st7701s_wsvga_dsi_vdo_checkers_st_kd_hsd on the 1st gen Show 5,
// ..._cronos_... on the 2nd, lcm=1-jd936x_wxga_dsi_vdo_crown_st_kd_hsd on the Show 8 (read off a
// unit 2026-09-22). No one of the three names contains another, so where there is a panel name it
// is the whole answer.
//
// Where there is none there is less to go on: amazon-gating tells the 2nd gen Show 5 from the other
// two, but not those two from each other, so a unit carrying it and naming no panel is read as the
// 1st gen Show 5. That is the safer way to be wrong about a Show 8, which shares its mute driver
// and its speaker codec and differs mainly in the panel and the microphone count.
//
// The 2nd gen Show 8 comes first, from its device tree: its kernel is Amazon's and names no panel.
func showBoard(cmdline, gating string, compatible []string) string {
	for _, c := range compatible {
		if c == athenaCompatible || strings.HasPrefix(c, athenaCompatible+"-") {
			return boardAthena
		}
	}
	for _, f := range strings.Fields(cmdline) {
		if v, ok := strings.CutPrefix(f, "lcm="); ok {
			switch {
			case strings.Contains(v, boardCheckers):
				return boardCheckers
			case strings.Contains(v, boardCrown):
				return boardCrown
			case strings.Contains(v, boardCronos):
				return boardCronos
			}
		}
	}
	if _, err := os.Stat(gating); err == nil {
		return boardCheckers
	}
	return boardCronos
}

func kernelCmdline() string {
	b, _ := os.ReadFile("/proc/cmdline")
	return string(b)
}

// deviceTreeCompatible is the root node's compatible list, which the kernel keeps as strings each
// ended by a NUL. Empty where there is no device tree to read, as on a workstation.
func deviceTreeCompatible() []string {
	b, err := os.ReadFile("/proc/device-tree/compatible")
	if err != nil {
		return nil
	}
	var out []string
	for _, s := range strings.Split(string(b), "\x00") {
		if s = strings.TrimSpace(s); s != "" {
			out = append(out, s)
		}
	}
	return out
}
