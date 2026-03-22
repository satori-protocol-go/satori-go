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
	return !o.ok
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
		*o = None[T]()
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
