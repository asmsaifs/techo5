//go:build !dot

package camstream

import (
	"github.com/HuskerMinion/techo5/echod/internal/hardware/camera"
	"github.com/HuskerMinion/techo5/echod/internal/hardware/lenscover"
)

// size is the picture the stream sends, as RTP/JPEG can say it: in eighths.
func size() (w, h int) { return camera.Width &^ 7, camera.Height &^ 7 }

// onCover has fn told when the lens cover closes.
func onCover(fn func()) (cancel func()) {
	return lenscover.Get().Changed.Listen(func(covered bool) {
		if covered {
			fn()
		}
	})
}
