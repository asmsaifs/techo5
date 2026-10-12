//go:build !dot

package video

import (
	"sync"
	"syscall"
)

// UseDecoderForTests is for other packages' tests (feature/home's, which start and end camera views and
// check that no decoder outlives one): path is run as the decoder, as the test's own user, with env as
// its whole environment and no limits, until restore is called. The daemon never calls it.
func UseDecoderForTests(path string, env []string) (restore func()) {
	savedPath, savedCred, savedLimits, savedEnv := ffmpegPath, decoderCred, limits, decoderEnv
	ffmpegPath, limits, decoderEnv = path, nil, env
	decoderCred = func() (*syscall.Credential, error) { return nil, nil }
	rtspOnce = sync.Once{}
	return func() {
		ffmpegPath, decoderCred, limits, decoderEnv = savedPath, savedCred, savedLimits, savedEnv
		rtspOnce = sync.Once{}
	}
}
