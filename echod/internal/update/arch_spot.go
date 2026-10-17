//go:build spot

package update

// archSuffix marks the Spot's builds, so a release carrying another device's binary offers this one
// nothing rather than the wrong thing.
const archSuffix = "-spot"

// rootfsArch is the key this device's root filesystem is published under: its architecture, as
// for its binary (arch_cronos.go has why the Show 8 2nd gen differs).
func rootfsArch() string { return arch }
