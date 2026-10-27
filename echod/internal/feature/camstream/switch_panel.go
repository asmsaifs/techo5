//go:build panel

package camstream

import (
	"github.com/HuskerMinion/techo5/echod/internal/hardware/camera"
	"github.com/HuskerMinion/techo5/echod/internal/lib/panelctl"
)

// The Panel app asks for camera permission only after someone enables its camera. Keep the setting
// across app restarts, and erase every captured image immediately when it is switched off.
func panelCameraSwitch(on bool) {
	if on {
		panelctl.Send("camera 1")
		return
	}
	panelctl.Send("camera 0")
	// Off in the background: a still or a stream being opened holds the camera's locks for up to
	// twelve seconds, and the switch must not hang for it. Nothing is served meanwhile, since every
	// path asks whether the camera is on, and that was saved before this.
	go camera.Get().Disable()
}
