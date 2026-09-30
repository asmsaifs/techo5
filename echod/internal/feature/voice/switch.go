package voice

// Which of the two assistants is live.
//
// The switch belongs here rather than in either backend, because this is the package that owns the
// thing both backends are reached through: a wake word, and the action button. Home Assistant's
// satellite is a pipeline this device dials into and xiaozhi is a socket this device holds, and
// neither of them can be told which of the two should answer without being handed the question — so
// the question is answered here, once, and both are asked.
//
// The invariant is one line: at most one backend holds the microphone or the speaker. It is enforced
// in three places rather than one, because three different events can break it and each needs
// ending rather than refusing:
//
//   - a wake word or a button, while the other backend has the room. The turn that was interrupted is
//     ended first. This is the barge-in case, and it is the same gesture either way: reaching for the
//     button means make it stop, whichever of them is talking.
//   - the switch moving, mid-turn. The backend being left behind is told to stand down.
//   - a turn opening on one backend while the other is mid-turn, from a path that did not come
//     through here — a terminal's `xiaozhi listen` over ssh, say. That one is handled on xiaozhi's
//     side (xiaozhi.YieldMic), because a package cannot be asked about a turn it does not own.
//
// The switch is not a third thing on top of the xiaozhi client's own on/off. Those answer different
// questions and both are kept: the client's switch asks whether this device may send microphone audio
// to a third party at all, and it starts off; this one asks which of two assistants the room is
// talking to, and it starts on Home Assistant. Turning the client off is therefore allowed while
// xiaozhi is the chosen backend, and a wake word in that state says so instead of opening a turn to
// nowhere.

import (
	"log/slog"
	"time"

	esphome "github.com/ygelfand/go-esphome-device"

	"github.com/HuskerMinion/techo5/echod/internal/component"
	"github.com/HuskerMinion/techo5/echod/internal/config"
	"github.com/HuskerMinion/techo5/echod/internal/feature/xiaozhi"
)

// Backend is which assistant a wake word or the action button opens a turn on.
//
// A function rather than a field because the answer is in the config, and a copy of it in this
// package would be a second thing to keep in step with the first. The config read is a mutex and a
// struct copy, on a path that is a wake word or a button press.
func Backend() config.VoiceBackend { return config.Get().Voice.Backend }

// OnXiaozhi reports whether xiaozhi is the live backend.
func OnXiaozhi() bool { return Backend() == config.BackendXiaozhi }

// ChooseBackend puts a backend in force from the settings sheet, which is the same three steps
// Home Assistant's select takes: move, save, publish.
//
// The save sits between them, and it is why the order is written out rather than shared with the
// select's. The sheet has to publish the value that is in force afterwards, and if the write failed
// that is not the value that was asked for — it is the one still standing, with the turn it just cut
// off resumed nowhere. Publishing the old value makes the row tell the truth about a device whose
// save failed, which is the only way anybody finds out it did.
func (v *Voice) ChooseBackend(b config.VoiceBackend) {
	was := Backend()
	settled := v.chooseBackend(b)
	if err := config.Set().Voice().Backend(settled); err != nil {
		slog.Error("saving the voice backend failed", "err", err)
		settled = was
	}
	v.backend.Set(settled.Label())
}

// BackendLabels are the two backends as the picker shows them, so the sheet offers the same words
// Home Assistant's select does rather than a second set of its own.
func BackendLabels() []string { return config.Labels(config.VoiceBackends()) }

// backends builds the select, and the arbitration that goes with it.
//
// The select and not a switch, because the two ends are peers and a switch would only ever name one
// of them: the value is asked for, not switched on. It sits on the device rather than on a
// sub-device, because there is one of it in the house per satellite and a sub-device for a single
// setting is an entry in the registry to click into for no reason.
func (v *Voice) backends() *esphome.Select {
	sel := &esphome.Select{
		Base: esphome.Base{
			ObjectID: "voice_backend", Name: "Voice backend", Icon: "mdi:account-voice",
			Category: esphome.CategoryConfig,
		},
	}
	// The save is wrapped rather than passed straight to component.Bind, because this select has
	// more to do when a write fails than the other selects do: it has just ended a turn, and a row
	// left showing the backend that was just switched away from is a row about a turn that no longer
	// exists. Publish on the way out either way, so the entity shows what is in force rather than
	// what was asked for.
	save := config.Set().Voice().Backend
	component.Bind(sel, config.VoiceBackends(), v.chooseBackend, func(b config.VoiceBackend) error {
		err := save(b)
		if err != nil {
			slog.Error("saving the voice backend failed", "err", err)
			sel.Set(Backend().Label())
		}
		return err
	})
	return sel
}

// chooseBackend is the move itself, which is the half both doors have in common: end the turn the
// backend being left behind was running, and report what the switch settled on.
//
// Saving is not here. The two doors that can move this — Home Assistant's select and the settings
// sheet — each save through the one setter that owns the file, so that a setting is written from one
// place whichever door moved it. See ChooseBackend for the sheet's half.
//
// The turn is ended before the setting is saved, and that order is the whole of the failure case. A
// save that fails — a full disk, a read-only file — after the turn was ended would leave a device
// that has cut off the answer it was giving and then reverted to the backend that was giving it.
// Ending first means a failed save leaves the device on the backend it was already on, mid-answer,
// which is a thing that happened rather than a thing that was lost.
func (v *Voice) chooseBackend(b config.VoiceBackend) config.VoiceBackend {
	settled := b.Settle()
	was := Backend()
	if settled == was {
		return settled
	}

	v.standDown(was)
	slog.Info("voice backend", "was", was, "now", settled)
	return settled
}

// standDown ends whatever the backend being left behind was doing.
//
// It is the half of the invariant that has to happen on a switch rather than on a gesture, and it
// reports whether there was anything to end, which is worth a log line of its own: a switch moved
// mid-answer is the case where the room would otherwise be left talking to a device that has
// stopped listening, and a device that says so in its log is a device somebody can debug.
func (v *Voice) standDown(was config.VoiceBackend) {
	switch was {
	case config.BackendXiaozhi:
		if xiaozhi.Get().StandDown() {
			slog.Info("the xiaozhi turn was ended by the switch")
		}
	default:
		if v.turn.Busy() {
			slog.Info("the Home Assistant turn was ended by the switch")
			v.turn.Cancel()
		}
	}
}

// takeMicrophone is what xiaozhi calls before it opens a turn, which is how a turn started anywhere
// on that backend — a wake word routed here, or a terminal's `xiaozhi listen` over ssh — is kept
// from talking over one already running on this one.
//
// Cancelling is enough rather than a Stop, and the difference is the speaker. A canceled turn runs
// its own ending — the listen is closed, the utterance is sent, the pipeline is told — and the
// answer it produces is not yet playing, so there is nothing to silence. A Stop would also reach for
// the speaker and, with no answer started, that is a claim taken and released for no reason.
//
// The reply that a canceled turn may still produce is not a leak: a turn that is interrupted is
// interrupted, and the pipeline is told so by the cancellation itself.
//
// It waits, because the microphone is one and the other side is about to claim it. Cancel is
// synchronous where the turn's own ending is not — the answer's player goroutine is told to stop
// separately — so returning before the turn has let go would hand over a microphone that is still
// held, and the two would interleave frames into a session the first one is still closing.
func (v *Voice) takeMicrophone() {
	if !v.turn.Busy() {
		return
	}
	slog.Info("a xiaozhi turn interrupted the Home Assistant one")
	v.turn.Cancel()

	// Bounded, because this runs on the goroutine that is opening the other turn, and an unbounded
	// wait for a turn that cannot end would wedge that one for good rather than merely fail it. The
	// handover goes ahead either way: a microphone two backends both think they hold is worse than
	// one they are both fighting over, and the log line is the evidence for which this is.
	deadline := time.Now().Add(micHandoverWait)
	for v.turn.Busy() && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if v.turn.Busy() {
		slog.Warn("the Home Assistant turn did not let go of the microphone, handing it over anyway",
			"waited", micHandoverWait)
	}
}

// micHandoverWait is how long a cancelled turn is given to let go of the microphone.
//
// Longer than any of the steps a cancelled turn has left to do — a close, a stop to the pipeline, an
// unlisten — and short enough that a turn stuck in one of them cannot hold the device. The steps are
// all local writes and a close on a socket this device holds, so none of them should take this long,
// and the one that would is a bug worth seeing rather than a wait worth extending.
const micHandoverWait = 2 * time.Second

// restore puts the switch back where the device was left.
//
// Not a turn and not a session: at start-up there is neither, so this is the setting and the log
// line. The entity is published rather than only read, because Home Assistant shows a select's value
// from what the device last sent, and a select that has never been sent shows blank until something
// changes it.
func (v *Voice) restore(c config.Config) {
	component.Restore(v.backend, c.Voice.Backend, v.chooseBackend)
}
