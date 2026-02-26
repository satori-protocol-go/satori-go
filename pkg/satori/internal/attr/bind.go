package attr

import (
	"fmt"
	"reflect"
	"strings"
)

// UnmarshalAttrs binds attrs to dst struct fields by cached metadata.
//
// Resolution order for each field is:
//  1. `attr` tag key
//  2. `json` tag key
//  3. derived field-name keys
//
// Fields are required by default. Mark optional with `omitempty` in tag options,
// e.g. `attr:"name,omitempty"` or `json:"name,omitempty"`.
func UnmarshalAttrs(dst any, attrs map[string]any) error {
	if attrs == nil {
		attrs = map[string]any{}
	}
	if dst == nil {
		return fmt.Errorf("dst is nil")
	}

	rv := reflect.ValueOf(dst)
	if rv.Kind() != reflect.Pointer || rv.IsNil() {
		return fmt.Errorf("dst must be a non-nil pointer")
	}
	rv = rv.Elem()
	if rv.Kind() != reflect.Struct {
		return fmt.Errorf("dst must point to struct")
	}

	meta := getTypeMeta(rv.Type())
	for _, f := range meta.fields {
		var (
			raw   any
			found bool
		)
		for _, key := range f.keys {
			var ok bool
			raw, ok = attrs[key]
			if ok {
				found = true
				break
			}
		}
		if !found {
			if f.required {
				return fmt.Errorf("missing required attr for field %s (keys: %s)", f.name, strings.Join(f.keys, ","))
			}
			continue
		}

		cv, err := convertValue(raw, f.typ)
		if err != nil {
			return fmt.Errorf("field %s: %w", f.name, err)
		}
		field := rv.FieldByIndex(f.index)
		if !field.CanSet() {
			continue
		}
		field.Set(cv)
	}
	return nil
}
