package attr

import (
	"fmt"
	"math"
	"reflect"
	"strconv"
)

func convertValue(value any, target reflect.Type) (reflect.Value, error) {
	if value == nil {
		return reflect.Zero(target), nil
	}

	raw := reflect.ValueOf(value)
	if raw.Type().AssignableTo(target) {
		return raw, nil
	}

	if target.Kind() == reflect.Pointer {
		elem, err := convertValue(value, target.Elem())
		if err != nil {
			return reflect.Value{}, err
		}
		ptr := reflect.New(target.Elem())
		ptr.Elem().Set(elem)
		return ptr, nil
	}

	switch target.Kind() {
	case reflect.Interface:
		if raw.Type().Implements(target) {
			return raw, nil
		}
		return raw, nil
	case reflect.String:
		return reflect.ValueOf(fmt.Sprint(value)).Convert(target), nil
	case reflect.Bool:
		return convertBool(value, target)
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return convertInt(value, target)
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr:
		return convertUint(value, target)
	case reflect.Float32, reflect.Float64:
		return convertFloat(value, target)
	case reflect.Slice:
		return convertSlice(value, target)
	case reflect.Map:
		return convertMap(value, target)
	case reflect.Struct:
		return convertStruct(value, target)
	default:
		if raw.Type().ConvertibleTo(target) {
			return raw.Convert(target), nil
		}
		return reflect.Value{}, fmt.Errorf("unsupported conversion: %T -> %s", value, target)
	}
}

func convertBool(value any, target reflect.Type) (reflect.Value, error) {
	switch v := value.(type) {
	case bool:
		return reflect.ValueOf(v).Convert(target), nil
	case string:
		parsed, err := strconv.ParseBool(v)
		if err != nil {
			return reflect.Value{}, err
		}
		return reflect.ValueOf(parsed).Convert(target), nil
	default:
		return reflect.Value{}, fmt.Errorf("cannot parse bool from %T", value)
	}
}

func convertInt(value any, target reflect.Type) (reflect.Value, error) {
	intValue, err := toInt64(value)
	if err != nil {
		return reflect.Value{}, err
	}
	out := reflect.New(target).Elem()
	out.SetInt(intValue)
	return out, nil
}

func convertUint(value any, target reflect.Type) (reflect.Value, error) {
	uintValue, err := toUint64(value)
	if err != nil {
		return reflect.Value{}, err
	}
	out := reflect.New(target).Elem()
	out.SetUint(uintValue)
	return out, nil
}

func convertFloat(value any, target reflect.Type) (reflect.Value, error) {
	floatValue, err := toFloat64(value)
	if err != nil {
		return reflect.Value{}, err
	}
	out := reflect.New(target).Elem()
	out.SetFloat(floatValue)
	return out, nil
}

func convertSlice(value any, target reflect.Type) (reflect.Value, error) {
	rv := reflect.ValueOf(value)
	if rv.Kind() != reflect.Slice && rv.Kind() != reflect.Array {
		return reflect.Value{}, fmt.Errorf("expected slice/array, got %T", value)
	}
	out := reflect.MakeSlice(target, rv.Len(), rv.Len())
	for i := range rv.Len() {
		elem, err := convertValue(rv.Index(i).Interface(), target.Elem())
		if err != nil {
			return reflect.Value{}, err
		}
		out.Index(i).Set(elem)
	}
	return out, nil
}

func convertMap(value any, target reflect.Type) (reflect.Value, error) {
	rv := reflect.ValueOf(value)
	if rv.Kind() != reflect.Map {
		return reflect.Value{}, fmt.Errorf("expected map, got %T", value)
	}
	out := reflect.MakeMapWithSize(target, rv.Len())
	iter := rv.MapRange()
	for iter.Next() {
		key, err := convertValue(iter.Key().Interface(), target.Key())
		if err != nil {
			return reflect.Value{}, err
		}
		val, err := convertValue(iter.Value().Interface(), target.Elem())
		if err != nil {
			return reflect.Value{}, err
		}
		out.SetMapIndex(key, val)
	}
	return out, nil
}

func convertStruct(value any, target reflect.Type) (reflect.Value, error) {
	if reflect.TypeOf(value).AssignableTo(target) {
		return reflect.ValueOf(value), nil
	}
	attrs, ok := value.(map[string]any)
	if !ok {
		return reflect.Value{}, fmt.Errorf("expected map[string]any for struct, got %T", value)
	}
	outPtr := reflect.New(target)
	if err := UnmarshalAttrs(outPtr.Interface(), attrs); err != nil {
		return reflect.Value{}, err
	}
	return outPtr.Elem(), nil
}

func toInt64(value any) (int64, error) {
	switch v := value.(type) {
	case int:
		return int64(v), nil
	case int8:
		return int64(v), nil
	case int16:
		return int64(v), nil
	case int32:
		return int64(v), nil
	case int64:
		return v, nil
	case uint:
		return int64(v), nil
	case uint8:
		return int64(v), nil
	case uint16:
		return int64(v), nil
	case uint32:
		return int64(v), nil
	case uint64:
		if v > uint64(math.MaxInt64) {
			return 0, fmt.Errorf("uint64 overflow for int64: %d", v)
		}
		return int64(v), nil
	case float32:
		return int64(v), nil
	case float64:
		return int64(v), nil
	case string:
		return strconv.ParseInt(v, 10, 64)
	default:
		return 0, fmt.Errorf("cannot parse int from %T", value)
	}
}

func toUint64(value any) (uint64, error) {
	switch v := value.(type) {
	case int:
		if v < 0 {
			return 0, fmt.Errorf("negative int for uint: %d", v)
		}
		return uint64(v), nil
	case int8:
		if v < 0 {
			return 0, fmt.Errorf("negative int8 for uint: %d", v)
		}
		return uint64(v), nil
	case int16:
		if v < 0 {
			return 0, fmt.Errorf("negative int16 for uint: %d", v)
		}
		return uint64(v), nil
	case int32:
		if v < 0 {
			return 0, fmt.Errorf("negative int32 for uint: %d", v)
		}
		return uint64(v), nil
	case int64:
		if v < 0 {
			return 0, fmt.Errorf("negative int64 for uint: %d", v)
		}
		return uint64(v), nil
	case uint:
		return uint64(v), nil
	case uint8:
		return uint64(v), nil
	case uint16:
		return uint64(v), nil
	case uint32:
		return uint64(v), nil
	case uint64:
		return v, nil
	case float32:
		if v < 0 {
			return 0, fmt.Errorf("negative float32 for uint: %f", v)
		}
		return uint64(v), nil
	case float64:
		if v < 0 {
			return 0, fmt.Errorf("negative float64 for uint: %f", v)
		}
		return uint64(v), nil
	case string:
		return strconv.ParseUint(v, 10, 64)
	default:
		return 0, fmt.Errorf("cannot parse uint from %T", value)
	}
}

func toFloat64(value any) (float64, error) {
	switch v := value.(type) {
	case int:
		return float64(v), nil
	case int8:
		return float64(v), nil
	case int16:
		return float64(v), nil
	case int32:
		return float64(v), nil
	case int64:
		return float64(v), nil
	case uint:
		return float64(v), nil
	case uint8:
		return float64(v), nil
	case uint16:
		return float64(v), nil
	case uint32:
		return float64(v), nil
	case uint64:
		return float64(v), nil
	case float32:
		return float64(v), nil
	case float64:
		return v, nil
	case string:
		return strconv.ParseFloat(v, 64)
	default:
		return 0, fmt.Errorf("cannot parse float from %T", value)
	}
}
