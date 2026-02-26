package attr

import (
	"reflect"
	"strings"
	"sync"
)

type fieldMeta struct {
	index    []int
	keys     []string
	typ      reflect.Type
	name     string
	required bool
}

type typeMeta struct {
	fields []fieldMeta
}

var metaCache sync.Map // map[reflect.Type]*typeMeta

func getTypeMeta(t reflect.Type) *typeMeta {
	if meta, ok := metaCache.Load(t); ok {
		return meta.(*typeMeta)
	}
	built := buildTypeMeta(t)
	actual, _ := metaCache.LoadOrStore(t, built)
	return actual.(*typeMeta)
}

func buildTypeMeta(t reflect.Type) *typeMeta {
	visible := reflect.VisibleFields(t)
	fields := make([]fieldMeta, 0, len(visible))
	for _, sf := range visible {
		if !isFieldBindable(sf) {
			continue
		}
		meta, ok := parseFieldMeta(sf)
		if !ok {
			continue
		}
		fields = append(fields, meta)
	}
	return &typeMeta{fields: fields}
}

func isFieldBindable(sf reflect.StructField) bool {
	// Skip unexported fields.
	if sf.PkgPath != "" {
		return false
	}
	// Skip embedded structs unless explicit tag is provided.
	if sf.Anonymous && sf.Tag.Get("attr") == "" && sf.Tag.Get("json") == "" {
		return false
	}
	return true
}

func parseFieldMeta(sf reflect.StructField) (fieldMeta, bool) {
	attrName, attrOpts, _, ignored := parseTag(sf.Tag.Get("attr"))
	if ignored {
		return fieldMeta{}, false
	}
	jsonName, jsonOpts, _, jsonIgnored := parseTag(sf.Tag.Get("json"))
	if jsonIgnored {
		jsonName = ""
	}

	keys := make([]string, 0, 4)
	if attrName != "" {
		keys = appendUnique(keys, attrName)
	}
	if jsonName != "" {
		keys = appendUnique(keys, jsonName)
	}
	fallback := fieldFallbackKeys(sf.Name)
	for _, key := range fallback {
		keys = appendUnique(keys, key)
	}
	if len(keys) == 0 {
		return fieldMeta{}, false
	}

	required := true
	if containsOption(attrOpts, "omitempty") || containsOption(jsonOpts, "omitempty") {
		required = false
	}

	return fieldMeta{
		index:    sf.Index,
		keys:     keys,
		typ:      sf.Type,
		name:     sf.Name,
		required: required,
	}, true
}

func parseTag(tag string) (name string, opts []string, set bool, ignored bool) {
	if tag == "" {
		return "", nil, false, false
	}
	set = true
	parts := strings.Split(tag, ",")
	name = strings.TrimSpace(parts[0])
	if name == "-" {
		return "", nil, true, true
	}
	if len(parts) > 1 {
		opts = make([]string, 0, len(parts)-1)
		for _, opt := range parts[1:] {
			opt = strings.TrimSpace(opt)
			if opt != "" {
				opts = append(opts, opt)
			}
		}
	}
	return name, opts, true, false
}

func containsOption(options []string, target string) bool {
	for _, opt := range options {
		if opt == target {
			return true
		}
	}
	return false
}

func fieldFallbackKeys(name string) []string {
	if name == "" {
		return nil
	}
	lowerFirst := strings.ToLower(name[:1]) + name[1:]
	if lowerFirst == name {
		return []string{name}
	}
	return []string{lowerFirst, name}
}

func appendUnique(slice []string, value string) []string {
	if value == "" {
		return slice
	}
	for _, item := range slice {
		if item == value {
			return slice
		}
	}
	return append(slice, value)
}
