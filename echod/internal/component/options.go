package component

import (
	"slices"
	"sync"

	esphome "github.com/ygelfand/go-esphome-device"
)

// Options keeps the options of a select whose list changes while the device runs (the players to
// follow, the weather sources, the dashboards). A new list goes into the select at once, through the
// library's locked SetOptions, and is offered to the device's own lists, which read Get, at once too.
// Home Assistant is told a select's options once, as it connects: a connection already up keeps the
// list it was given until the next one.
type Options struct {
	sel *esphome.Select

	mu  sync.Mutex
	now []string
}

// NewOptions keeps sel's options from now on; nothing else is to write sel.Options.
func NewOptions(sel *esphome.Select) *Options {
	return &Options{sel: sel, now: sel.Options}
}

// Get is the options offered now. The slice is shared: it is not to be changed.
func (o *Options) Get() []string {
	o.mu.Lock()
	defer o.mu.Unlock()
	return o.now
}

// Set offers opts from now on (to Home Assistant from its next connection), and says whether they
// differ from those offered before. opts is kept as it is, so it is not to be changed afterward.
func (o *Options) Set(opts []string) bool {
	o.mu.Lock()
	defer o.mu.Unlock()
	if slices.Equal(opts, o.now) {
		return false
	}
	o.now = opts
	o.sel.SetOptions(opts)
	return true
}

// Restore puts opts in the select, changed or not (Restorer.Restore).
func (o *Options) Restore(opts []string) {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.now = opts
	o.sel.SetOptions(opts)
}
