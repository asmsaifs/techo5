package config

import "reflect"

// deepCopy is c with nothing shared with it: every slice, map and pointer in it copied too, so a change
// made in place to one (an alarm in the list, a camera's address) does not show in the other. What
// UpdateOrUndo takes back a failed change to.
func deepCopy(c Config) Config {
	var out Config
	copyValue(reflect.ValueOf(&out).Elem(), reflect.ValueOf(c))
	return out
}

// copyValue sets dst, which can be set, to a copy of src sharing nothing with it.
func copyValue(dst, src reflect.Value) {
	switch src.Kind() {
	case reflect.Struct:
		dst.Set(src) // the plain fields, and any that cannot be set one by one
		for i := range src.NumField() {
			if f := dst.Field(i); f.CanSet() {
				copyValue(f, src.Field(i))
			}
		}
	case reflect.Slice:
		if src.IsNil() {
			dst.Set(reflect.Zero(src.Type()))
			return
		}
		s := reflect.MakeSlice(src.Type(), src.Len(), src.Len())
		for i := range src.Len() {
			copyValue(s.Index(i), src.Index(i))
		}
		dst.Set(s)
	case reflect.Map:
		if src.IsNil() {
			dst.Set(reflect.Zero(src.Type()))
			return
		}
		m := reflect.MakeMapWithSize(src.Type(), src.Len())
		for it := src.MapRange(); it.Next(); {
			v := reflect.New(src.Type().Elem()).Elem()
			copyValue(v, it.Value())
			m.SetMapIndex(it.Key(), v)
		}
		dst.Set(m)
	case reflect.Pointer:
		if src.IsNil() {
			dst.Set(reflect.Zero(src.Type()))
			return
		}
		p := reflect.New(src.Type().Elem())
		copyValue(p.Elem(), src.Elem())
		dst.Set(p)
	case reflect.Array:
		for i := range src.Len() {
			copyValue(dst.Index(i), src.Index(i))
		}
	default:
		dst.Set(src)
	}
}
