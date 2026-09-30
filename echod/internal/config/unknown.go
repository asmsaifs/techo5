package config

import (
	"encoding/json"
	"reflect"
	"strings"
)

// Settings this build does not know are kept. A device can run an older build than the one that wrote
// its file - an update rolled back, or a test build before the release - and a build that wrote only
// what it knew threw the newer settings away the first time it saved anything: a device that went back
// a version and forward again came back with its voice assistant, its place and its calendars gone.
//
// Only keys this build has no field for are carried over. One it does know is written as it stands, so
// clearing a setting (an omitempty field left empty) still clears it.

// keepUnknown copies into out, the file about to be written, every key of was, the file as it was
// read, that type t has no field for, at every level of nested objects. Lists are this build's.
func keepUnknown(out, was map[string]any, t reflect.Type) {
	for t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	if t.Kind() != reflect.Struct {
		return
	}
	fields := jsonFields(t)
	for k, v := range was {
		ft, known := fields[k]
		if !known {
			out[k] = v
			continue
		}
		inner, ok1 := v.(map[string]any)
		now, ok2 := out[k].(map[string]any)
		if ok1 && ok2 {
			keepUnknown(now, inner, ft)
		}
	}
}

// jsonFields is a struct's fields by the name they are written under.
func jsonFields(t reflect.Type) map[string]reflect.Type {
	out := map[string]reflect.Type{}
	for i := range t.NumField() {
		f := t.Field(i)
		if !f.IsExported() {
			continue
		}
		name := f.Name
		if tag, ok := f.Tag.Lookup("json"); ok {
			n, _, _ := strings.Cut(tag, ",")
			if n == "-" {
				continue
			}
			if n != "" {
				name = n
			}
		}
		out[name] = f.Type
	}
	return out
}

// withUnknown is c written with what was read kept where this build has no field for it.
func withUnknown(c Config, was map[string]any) ([]byte, error) {
	b, err := json.Marshal(c)
	if err != nil || len(was) == 0 {
		if err != nil {
			return nil, err
		}
		return json.MarshalIndent(c, "", "  ")
	}
	var out map[string]any
	if err := json.Unmarshal(b, &out); err != nil {
		return nil, err
	}
	keepUnknown(out, was, reflect.TypeOf(c))
	return json.MarshalIndent(out, "", "  ")
}
