package codec

import (
	"strconv"
	"strings"
)

func firstNonEmpty(items ...string) string {
	for _, item := range items {
		if strings.TrimSpace(item) != "" {
			return item
		}
	}
	return ""
}

func optionalString(values map[string]any, key string) string {
	if values == nil {
		return ""
	}
	raw, ok := values[key]
	if !ok {
		return ""
	}
	text, ok := raw.(string)
	if !ok {
		return ""
	}
	return strings.TrimSpace(text)
}

func numberToInt(raw any) (int, bool) {
	switch typed := raw.(type) {
	case int:
		return typed, true
	case int8:
		return int(typed), true
	case int16:
		return int(typed), true
	case int32:
		return int(typed), true
	case int64:
		return int(typed), true
	case uint:
		return int(typed), true
	case uint8:
		return int(typed), true
	case uint16:
		return int(typed), true
	case uint32:
		return int(typed), true
	case uint64:
		return int(typed), true
	case float64:
		return int(typed), true
	case float32:
		return int(typed), true
	case string:
		number, err := strconv.Atoi(strings.TrimSpace(typed))
		if err == nil {
			return number, true
		}
	}
	return 0, false
}

func numberToUint32(raw any) (uint32, bool) {
	number, ok := numberToInt(raw)
	if !ok || number < 0 {
		return 0, false
	}
	return uint32(number), true
}
