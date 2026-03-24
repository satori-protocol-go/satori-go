package protocol

import "strings"

const InternalApiPrefix = "internal/"

func NormalizeInternalApi(raw string) string {
	value := strings.TrimSpace(raw)
	value = strings.Trim(value, "/")
	if value == "" {
		return ""
	}
	if strings.HasPrefix(value, InternalApiPrefix) {
		rest := strings.TrimPrefix(value, InternalApiPrefix)
		rest = strings.TrimLeft(rest, "/")
		if rest == "" {
			return InternalApiPrefix
		}
		return InternalApiPrefix + rest
	}
	return InternalApiPrefix + value
}
