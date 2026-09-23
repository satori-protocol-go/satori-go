package types

import (
	"bytes"
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
)

const MaxJSSafeInteger int64 = 9007199254740991

type Option[T any] struct {
	value T
	ok    bool
	null  bool
}

func Some[T any](value T) Option[T] {
	return Option[T]{
		value: value,
		ok:    true,
	}
}

func None[T any]() Option[T] {
	return Option[T]{}
}

func (o Option[T]) Get() (T, bool) {
	return o.value, o.ok
}

func (o Option[T]) IsSome() bool {
	return o.ok
}

func (o Option[T]) IsNone() bool {
	return !o.ok && !o.null
}

func (o Option[T]) ValueOr(defaultValue T) T {
	if !o.ok {
		return defaultValue
	}
	return o.value
}

func (o *Option[T]) UnmarshalJSON(data []byte) error {
	if o == nil {
		return nil
	}
	raw := strings.TrimSpace(string(data))
	if raw == "" || raw == "null" {
		*o = Option[T]{null: raw == "null"}
		return nil
	}

	var value T
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	if err := decoder.Decode(&value); err != nil {
		return err
	}

	normalized, err := normalizeOptionValue(value)
	if err != nil {
		return err
	}
	o.value = normalized
	o.ok = true
	o.null = false
	return nil
}

func normalizeOptionValue[T any](value T) (T, error) {
	normalized, err := trimStringLikeValue(value)
	if err != nil {
		return value, err
	}

	if err := validateOptionInteger(normalized); err != nil {
		return value, err
	}
	return normalized, nil
}

func trimStringLikeValue[T any](value T) (T, error) {
	ref := reflect.ValueOf(value)
	if !ref.IsValid() || ref.Kind() != reflect.String {
		return value, nil
	}

	trimmed := strings.TrimSpace(ref.String())
	holder := reflect.New(ref.Type()).Elem()
	holder.SetString(trimmed)
	converted, ok := holder.Interface().(T)
	if !ok {
		return value, fmt.Errorf("failed to normalize option string value")
	}
	return converted, nil
}

func validateOptionInteger[T any](value T) error {
	ref := reflect.ValueOf(value)
	if !ref.IsValid() {
		return nil
	}
	for ref.Kind() == reflect.Pointer {
		if ref.IsNil() {
			return nil
		}
		ref = ref.Elem()
	}

	switch ref.Kind() {
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		integer := ref.Int()
		if integer < -MaxJSSafeInteger || integer > MaxJSSafeInteger {
			return fmt.Errorf("integer exceeds JavaScript safe integer range")
		}
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr:
		if ref.Uint() > uint64(MaxJSSafeInteger) {
			return fmt.Errorf("integer exceeds JavaScript safe integer range")
		}
	}
	return nil
}

func OptionFromPointer[T any](pointer *T) Option[T] {
	if pointer == nil {
		return None[T]()
	}
	return Some(*pointer)
}

// IsNull distinguishes an explicitly supplied null from an absent value.
func (o Option[T]) IsNull() bool { return o.null }
func (o Option[T]) IsZero() bool { return o.IsNone() }
func (o Option[T]) MarshalJSON() ([]byte, error) {
	if !o.ok {
		return []byte("null"), nil
	}
	return json.Marshal(o.value)
}

// FieldPresence records presence/null, not unknown values. Resource codecs
// explicitly enumerate their own fields instead of merging unknown JSON.
type FieldPresence map[string]bool

func DecodeFields(data []byte, value any) (FieldPresence, error) {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	if err := decoder.Decode(value); err != nil {
		return nil, err
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(data, &fields); err != nil {
		return nil, err
	}
	result := make(FieldPresence, len(fields))
	for name, raw := range fields {
		result[name] = !bytes.Equal(bytes.TrimSpace(raw), []byte("null"))
	}
	return result, nil
}
func (p FieldPresence) Has(name string) bool  { _, ok := p[name]; return ok }
func (p FieldPresence) Null(name string) bool { value, ok := p[name]; return ok && !value }
func (p FieldPresence) Put(out map[string]any, name string, value any, nonzero bool) {
	present, exists := p[name]
	if nonzero || present {
		out[name] = value
	} else if exists {
		out[name] = nil
	}
}
func (p FieldPresence) Without(names ...string) FieldPresence {
	result := make(FieldPresence, len(p))
	for name, value := range p {
		result[name] = value
	}
	for _, name := range names {
		delete(result, name)
	}
	return result
}
