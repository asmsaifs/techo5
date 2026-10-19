//go:build dot || spot

package home

import esphome "github.com/ygelfand/go-esphome-device"

// gridActions is none: the grid is the Show's.
func (f *Feature) gridActions() []*esphome.Action { return nil }
