package server

import (
	"errors"
	"net/http"
	"strconv"
	"strings"
)

// ErrorHeaders forwards only bounded, non-secret diagnostic headers across the RPC boundary.
func ErrorHeaders(err error) http.Header {
	result := make(http.Header)
	var source interface{ ResponseHeaders() http.Header }
	if !errors.As(err, &source) {
		return result
	}
	values := source.ResponseHeaders()
	for _, key := range []string{"X-Tps-trace-ID", "X-QQ-Error-Code", "X-QQ-Upload-Stage", "X-QQ-Upload-Part"} {
		value := values.Get(key)
		if len(value) > 0 && len(value) <= 256 && !strings.ContainsAny(value, "\r\n") {
			result.Set(key, value)
		}
	}
	value := values.Get("Retry-After")
	if seconds, err := strconv.ParseInt(value, 10, 64); err == nil && seconds >= 0 {
		result.Set("Retry-After", value)
	} else if _, err := http.ParseTime(value); err == nil {
		result.Set("Retry-After", value)
	}
	return result
}

func (e *ActionError) ResponseHeaders() http.Header {
	if e == nil {
		return nil
	}
	result := ErrorHeaders(e.Err)
	for key, values := range e.Header {
		result[key] = append([]string(nil), values...)
	}
	return result
}
