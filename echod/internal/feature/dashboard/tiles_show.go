//go:build !dot && !spot

package dashboard

// hasTiles is whether the Dashboard tiles setting is offered: on the Show, whose wide screen a
// few big tiles can fill. The Spot's round one draws a single column either way.
const hasTiles = true

// screenWidth is the widest screen of the Shows, the Show 8's: as wide as a picture that may fill
// it is kept.
const screenWidth = 1280
