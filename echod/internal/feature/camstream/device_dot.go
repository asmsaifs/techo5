//go:build dot

package camstream

// size is nothing: the Dot has no camera.
func size() (w, h int) { return 0, 0 }

// onCover is nothing: no camera, no cover.
func onCover(func()) (cancel func()) { return func() {} }
