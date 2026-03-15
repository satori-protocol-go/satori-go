package eventconv

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	botgodto "github.com/WindowsSov8forUs/botgo-plus/dto"
	"github.com/satori-protocol-go/satori-go/pkg/satori/model/user"
)

func decodeWebhookData(raw json.RawMessage) (map[string]any, error) {
	if len(bytes.TrimSpace(raw)) == 0 {
		return map[string]any{}, nil
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	data := map[string]any{}
	if err := decoder.Decode(&data); err != nil {
		return nil, err
	}
	return data, nil
}

func decodeDTOMessage(raw map[string]any) *botgodto.Message {
	result := &botgodto.Message{}
	if !decodeInto(raw, result) {
		return &botgodto.Message{}
	}
	return result
}

func decodeDTOUser(c *Converter, raw map[string]any) *user.User {
	dtoUser := &botgodto.User{}
	if !decodeInto(raw, dtoUser) {
		return nil
	}
	return c.userFromDTO(dtoUser)
}

func decodeInto(raw map[string]any, target any) bool {
	if raw == nil {
		return false
	}
	payload, err := json.Marshal(raw)
	if err != nil {
		return false
	}
	return json.Unmarshal(payload, target) == nil
}

func objectValue(raw any) map[string]any {
	obj, ok := raw.(map[string]any)
	if !ok {
		return map[string]any{}
	}
	return obj
}

func valueAsString(raw any) string {
	switch typed := raw.(type) {
	case string:
		return strings.TrimSpace(typed)
	case json.Number:
		return typed.String()
	default:
		if raw == nil {
			return ""
		}
		return strings.TrimSpace(fmt.Sprint(raw))
	}
}

func pickTimestampOrDefault(candidate int64, data map[string]any) int64 {
	if candidate > 0 {
		return candidate
	}
	return pickEventTimestamp(data)
}

func pickEventTimestamp(data map[string]any) int64 {
	if ts, ok := parseTimestampAny(data["timestamp"]); ok {
		return ts
	}
	if ts, ok := parseTimestampAny(data["event_ts"]); ok {
		return ts
	}
	return currentTimestamp()
}

func parseTimestampAny(raw any) (int64, bool) {
	switch typed := raw.(type) {
	case nil:
		return 0, false
	case int:
		return normalizeEpoch(int64(typed)), true
	case int8:
		return normalizeEpoch(int64(typed)), true
	case int16:
		return normalizeEpoch(int64(typed)), true
	case int32:
		return normalizeEpoch(int64(typed)), true
	case int64:
		return normalizeEpoch(typed), true
	case uint:
		return normalizeEpoch(int64(typed)), true
	case uint8:
		return normalizeEpoch(int64(typed)), true
	case uint16:
		return normalizeEpoch(int64(typed)), true
	case uint32:
		return normalizeEpoch(int64(typed)), true
	case uint64:
		return normalizeEpoch(int64(typed)), true
	case float64:
		return normalizeEpoch(int64(typed)), true
	case float32:
		return normalizeEpoch(int64(typed)), true
	case json.Number:
		if number, err := typed.Int64(); err == nil {
			return normalizeEpoch(number), true
		}
		if number, err := typed.Float64(); err == nil {
			return normalizeEpoch(int64(number)), true
		}
	case string:
		return parseTimestamp(typed)
	}
	return 0, false
}

func parseTimestamp(raw string) (int64, bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return 0, false
	}
	if number, err := strconv.ParseInt(raw, 10, 64); err == nil {
		return normalizeEpoch(number), true
	}
	if number, err := strconv.ParseFloat(raw, 64); err == nil {
		return normalizeEpoch(int64(number)), true
	}
	if parsed, err := time.Parse(time.RFC3339Nano, raw); err == nil {
		return parsed.UnixMilli(), true
	}
	if parsed, err := time.Parse(time.RFC3339, raw); err == nil {
		return parsed.UnixMilli(), true
	}
	return 0, false
}

func normalizeEpoch(value int64) int64 {
	abs := value
	if abs < 0 {
		abs = -abs
	}
	if abs >= 1_000_000_000_000 {
		return value
	}
	if abs >= 1_000_000_000 {
		return value * 1000
	}
	return value
}

func currentTimestamp() int64 {
	return time.Now().UnixMilli()
}

func firstNonEmpty(items ...string) string {
	for _, item := range items {
		if strings.TrimSpace(item) != "" {
			return item
		}
	}
	return ""
}
