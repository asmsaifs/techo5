//go:build !dot

package led

// HasRing is whether the device has a ring to drive. The Echo Show and the Echo Spot have none, so the
// entities that set what a ring shows are not offered there: they would be controls for nothing.
const HasRing = false
