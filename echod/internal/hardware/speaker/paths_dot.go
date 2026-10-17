//go:build dot

package speaker

import (
	"math"
	"os"
	"strings"
	"time"

	"github.com/HuskerMinion/techo5/echod/internal/config"
	"github.com/HuskerMinion/techo5/echod/internal/layout"
)

// Output is one of the device's audio outputs.
type Output string

const (
	OutputSpeaker   Output = "speaker"
	OutputHeadphone Output = "headphone"
	// OutputBoth is the speaker and the jack at once, which neither board does (HasBoth).
	OutputBoth Output = "both"
)

// The mixer sequences below come direct from the device.
type kctl struct {
	name  string
	value string
	level int32
	blob  []byte
	// ifPresent writes the control only on a unit that has it: a part that differs between units of
	// the same model, rather than one that should always be there.
	ifPresent bool
}

// One build plays through both boards' codecs, which are the same part on the same card. The Echo
// 2nd gen (radar) differs behind it: its speaker has an amplifier of its own, gated by the codec's
// MFP2 pin (MuteSwitch), and its filters and paths are its own (paths_radar_dot.go).
var initSequence, pathSequence, headphoneOff = boardPaths(layout.Radar(), vendorMixerFiles)

func boardPaths(radar bool, files []string) ([]kctl, map[Output][]kctl, []kctl) {
	if !radar {
		return dotInit, dotPaths, dotHeadphoneOff
	}
	speaker, jack := radarEQ(files)
	return radarInit, radarPaths(speaker, jack), radarHeadphoneOff
}

// onRadar picks the Echo 2nd gen's value over the Dot's.
func onRadar[T any](radar, dot T) T {
	if layout.Radar() {
		return radar
	}
	return dot
}

var dotInit = []kctl{
	{name: AmpSwitch, value: "Off"},
	{name: "Audio_DacMux_Setting", value: "On"},
	{name: "Ignore Ramp Up", value: "Off"},
	{name: driverGain, level: 0},
	{name: eqControl, blob: speakerEQ},
}

// eqControl is the DAC's filter chain.
const eqControl = "biquad coefficients"

// speakerEQ is the DAC's filter chain: six unity blocks and one tuned filter, which is the vendor's
// tuning for this speaker. The coefficients read back as zeros until something writes them.
var speakerEQ = []byte{
	128, 0, 1, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0,
	128, 0, 1, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0,
	128, 0, 1, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0,
	128, 0, 1, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0,
	128, 0, 1, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0,
	128, 0, 1, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0,
	127, 247, 0, 0, 128, 9, 0, 0, 127, 239, 0, 0, 0, 17, 0, 0,
	0, 17, 0, 0, 127, 222, 0, 0, 15, 0, 0,
}

var dotPaths = map[Output][]kctl{
	OutputSpeaker: {
		{name: "HPL Output Mixer L_DAC Switch", level: 1},
		{name: "HPR Output Mixer R_DAC Switch", level: 1},
		{name: "Audio_DacMux_Setting", value: "Off"},
		{name: "Right Channel Only", value: "On"},
		{name: driverGain, level: 6},
	},
	OutputHeadphone: {
		// The DAC reaches the jack through these too, and the codec boots with them off: without them
		// here, a Dot that starts on the jack is silent until the speaker path has run once.
		{name: "HPL Output Mixer L_DAC Switch", level: 1},
		{name: "HPR Output Mixer R_DAC Switch", level: 1},
		{name: "Ignore Ramp Up", value: "On"},
		{name: driverGain, level: 11},
		{name: "Audio_DacMux_Setting", value: "On"},
		{name: "Right Channel Only", value: "Off"},
	},
}

// dotHeadphoneOff is the ext_headphone_output turnoff sequence.
var dotHeadphoneOff = []kctl{
	{name: "Audio_DacMux_Setting", value: "Off"},
	{name: "Right Channel Only", value: "On"},
	{name: "Ignore Ramp Up", value: "Off"},
}

const driverGain = "HP Driver Gain Volume"

// jackState is the kernel's headphone jack switch: 1 while something is plugged in.
const jackState = "/sys/class/switch/h2w/state"

// jackPoll is how often the jack switch is sampled.
const jackPoll = 500 * time.Millisecond

// DetectOutput picks the output to use. A missing switch means no jack detection, so assume the
// speaker.
func DetectOutput() Output {
	b, err := os.ReadFile(jackState)
	if err != nil || strings.TrimSpace(string(b)) == "0" {
		return OutputSpeaker
	}
	return OutputHeadphone
}

// VolumeSteps is the number of volume steps, matching the vendor's curves. The range is config's,
// since that is what a stored volume is in.
const VolumeSteps = config.VolumeSteps

// volumeCurves are the board's volume curves: index is the volume step, value is attenuation in dB.
var volumeCurves = onRadar(radarCurves, dotCurves)

// The vendor's volume curves, direct from the device.
var dotCurves = map[Output][VolumeSteps + 1]float64{
	OutputSpeaker: {
		-90, -33, -30, -26, -25, -23, -21, -19, -17, -16,
		-14, -13, -12, -10, -9, -8, -7, -5, -5, -4,
		-4, -4, -4, -3, -3, -3, -3, -3, -2, -1, 0,
	},
	OutputHeadphone: {
		-100, -39, -38, -36, -34, -33, -31, -29, -27, -26,
		-25, -24, -23, -21, -20, -19, -18, -17, -15, -14,
		-13, -12, -11, -9, -8, -7, -6, -4, -3, -2, 0,
	},
}

// mute is the attenuation the curves use for step 0.
const mute = -90

// gainForStep converts a volume step to a linear gain using the output's curve.
func gainForStep(out Output, step int) float32 {
	curve, ok := volumeCurves[out]
	if !ok {
		curve = volumeCurves[OutputSpeaker]
	}
	step = max(0, min(step, VolumeSteps))

	db := curve[step]
	if db <= mute {
		return 0
	}
	return float32(math.Pow(10, db/20))
}

// The playback ring on the Dot.
const (
	period  = 1024
	periods = 4
)

// DRAMHold is unused on the Dot: its DL1 driver's SRAM ring works.
const DRAMHold = ""

// startFull is off on the Dot: its kernel and DL1 driver are not the Show's, and it has no report of
// the restart storm the Show's had (paths_cronos.go).
const startFull = false

// softRef is off: the card's own loopback is the echo reference.
const softRef = false

// openSequence is empty: the playback device opens with the card as the kernel left it.
var openSequence []kctl

// MediaService is the init service that owns Android's audio HAL on Fire OS.
const MediaService = "media"

// AmpSwitch gates the speaker.
const AmpSwitch = "Ext_Speaker_Amp_Switch"

// MuteSwitch is the Echo 2nd gen's amplifier mute (radarMute), On while the path is set up and
// whenever the speaker is not playing. The Dot has none.
var MuteSwitch = onRadar(radarMute, "")

// mayHaveTAS5805M is whether a board of this build can carry the TAS5805M amplifier (GainAvailable):
// the Dot plays through its TLV320AIC32x4 and an external amplifier gated by AmpSwitch.
const mayHaveTAS5805M = false

// OutputBoost is make-up gain on the output; the Dot's vendor tuning already carries its own.
const OutputBoost = 1.0

// DriverTuning applies the vendor driver's volume-dependent EQ and limiter (lib/asp), read from the
// vendor partition: the files it reads are the Dot's. The Echo 2nd gen's are another kind (longer
// filters, one per volume step), which lib/asp does not read yet, so it plays untuned.
var DriverTuning = !layout.Radar()

// HasJack is whether the device has a headphone jack, and so the Audio output choice. Both boards
// have one; the Echo 2nd gen's is mono.
const HasJack = true

// HasBoth is whether the speaker and the jack can play at once, and so the Both choice. Not here:
// the routes have not been tried together on either board.
const HasBoth = false

// PlaybackDevice is the codec's playback PCM, which the card takes at Rate as it is: hwRate is Rate
// and nothing is trimmed after the volume (see paths_cronos.go for the board where neither holds).
const (
	PlaybackDevice = 23
	hwRate         = Rate
	hwTrimDB       = 0.0
)
