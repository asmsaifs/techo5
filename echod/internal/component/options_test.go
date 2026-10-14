package component

import (
	"slices"
	"testing"

	esphome "github.com/ygelfand/go-esphome-device"
)

// A new list goes into the select at once and is offered to the device's own lists; the same list
// is not a change.
func TestOptionsChange(t *testing.T) {
	sel := &esphome.Select{Options: []string{"a"}}
	o := NewOptions(sel)
	if o.Set([]string{"a"}) {
		t.Error("the same list counted as a change")
	}
	if !o.Set([]string{"a", "b"}) {
		t.Error("a new list did not count as a change")
	}
	if !slices.Equal(o.Get(), []string{"a", "b"}) {
		t.Errorf("offered %q", o.Get())
	}
	if !slices.Equal(sel.Options, []string{"a", "b"}) {
		t.Errorf("the select: %q", sel.Options)
	}

	o.Restore([]string{"c"})
	if !slices.Equal(sel.Options, []string{"c"}) || !slices.Equal(o.Get(), []string{"c"}) {
		t.Errorf("restored: select %q, offered %q", sel.Options, o.Get())
	}
	if o.Set([]string{"c"}) {
		t.Error("the restored list counted as a change")
	}
}
