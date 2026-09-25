package logging

import (
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"strconv"
	"strings"

	"github.com/satori-protocol-go/satori-go/pkg/satori/model/login"
	"github.com/satori-protocol-go/satori-go/pkg/satori/model/operation"
)

var diagnosticURL = regexp.MustCompile(`(?i)(?:https?|wss?)://[^\s<>"']+|(?:data:|internal:)[^\s<>"']+`)
var diagnosticCredential = regexp.MustCompile(`(?i)((?:access_token|app_secret|authorization|client_secret|refresh_token|token|secret)["']?\s*[=:]\s*)(?:"[^"]*"|'[^']*'|(?:Bearer|QQBot|Bot)\s+[^\s,;}]+|[^\s,;}]+)`)

// SafeText returns a single-line diagnostic value, never a format string.
func SafeText(value string) string {
	value = diagnosticURL.ReplaceAllStringFunc(value, Endpoint)
	value = diagnosticCredential.ReplaceAllString(value, "${1}[redacted]")
	var out strings.Builder
	for _, r := range value {
		if r < 32 || r == 127 || r == '\u2028' || r == '\u2029' {
			quoted := strconv.QuoteRune(r)
			out.WriteString(quoted[1 : len(quoted)-1])
		} else {
			out.WriteRune(r)
		}
	}
	return out.String()
}

// Endpoint omits paths, user information, queries and fragments from resource addresses.
func Endpoint(raw string) string {
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" {
		return "[address omitted]"
	}
	switch strings.ToLower(u.Scheme) {
	case "http", "https", "ws", "wss":
		return u.Scheme + "://" + u.Host
	default:
		return "[address omitted]"
	}
}

// ErrorText only formats diagnostics; the original error and its chain remain unchanged.
func ErrorText(err error) string { return errorText(err, 0) }

func errorText(err error, depth int) string {
	if err == nil {
		return ""
	}
	if depth >= 16 {
		return "further error details omitted"
	}
	if joined, ok := err.(interface{ Unwrap() []error }); ok {
		var parts []string
		for _, cause := range joined.Unwrap() {
			if cause != nil {
				parts = append(parts, errorText(cause, depth+1))
			}
		}
		return strings.Join(parts, "; ")
	}
	var response interface {
		HTTPStatus() int
		ResponseBody() string
	}
	if errors.As(err, &response) {
		return fmt.Sprintf("request returned HTTP %d (response body omitted)", response.HTTPStatus())
	}
	var request *url.Error
	if errors.As(err, &request) && request != nil {
		return SafeText(request.Op) + " " + Endpoint(request.URL) + ": " + errorText(request.Err, depth+1)
	}
	text := err.Error()
	// sendWebhook preserves the response body in its returned error. Do not copy that body into logs.
	if strings.HasPrefix(text, "webhook response status ") {
		head, _, _ := strings.Cut(text, ":")
		return SafeText(head) + " (response body omitted)"
	}
	text = SafeText(text)
	runes := []rune(text)
	if len(runes) > 2048 {
		return string(runes[:2048]) + "..."
	}
	return text
}

func FrameName(op operation.Opcode) string {
	switch op {
	case operation.OpcodeEvent:
		return "EVENT"
	case operation.OpcodePing:
		return "PING"
	case operation.OpcodePong:
		return "PONG"
	case operation.OpcodeIdentify:
		return "IDENTIFY"
	case operation.OpcodeReady:
		return "READY"
	case operation.OpcodeMeta:
		return "META"
	default:
		return fmt.Sprintf("unknown opcode %d", op)
	}
}

func LoginStatusName(status login.LoginStatus) string {
	switch status {
	case login.LoginStatusOffline:
		return "offline"
	case login.LoginStatusOnline:
		return "online"
	case login.LoginStatusConnect:
		return "connecting"
	case login.LoginStatusDisconnect:
		return "disconnecting"
	case login.LoginStatusReconnect:
		return "reconnecting"
	default:
		return fmt.Sprintf("unknown status %d", status)
	}
}
