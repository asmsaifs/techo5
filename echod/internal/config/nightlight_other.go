//go:build !spot

package config

// rescaleNightLight does nothing off the Spot: only the Spot's night clock changed scale.
func (c *Config) rescaleNightLight() {}
