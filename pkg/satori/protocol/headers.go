package protocol

import (
	"errors"
	"net/http"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"
)

const (
	HeaderAuthorization  = "Authorization"
	HeaderPlatform       = "X-Platform"
	HeaderSelfID         = "X-Self-ID"
	HeaderSatoriPlatform = "Satori-Platform"
	HeaderSatoriUserID   = "Satori-User-ID"
	HeaderOpcode         = "Satori-OpCode"
)

const BearerScheme = "Bearer"

var (
	ErrMissingPlatformHeader = errors.New("missing header X-Platform or Satori-Platform")
	ErrMissingSelfIDHeader   = errors.New("missing header X-Self-ID or Satori-User-ID")
)

func SetIdentityHeaders(header http.Header, platform string, selfID string) {
	if header == nil {
		return
	}
	header.Set(HeaderPlatform, platform)
	header.Set(HeaderSelfID, selfID)
	header.Set(HeaderSatoriPlatform, platform)
	header.Set(HeaderSatoriUserID, selfID)
}

func ExtractIdentityHeaders(header http.Header) (string, string, error) {
	platform := header.Get(HeaderSatoriPlatform)
	if platform == "" {
		platform = header.Get(HeaderPlatform)
	}
	if platform == "" {
		return "", "", ErrMissingPlatformHeader
	}

	selfID := header.Get(HeaderSatoriUserID)
	if selfID == "" {
		selfID = header.Get(HeaderSelfID)
	}
	if selfID == "" {
		return "", "", ErrMissingSelfIDHeader
	}
	return platform, selfID, nil
}

func SetBearer(header http.Header, token string) {
	if header == nil {
		return
	}
	header.Set(HeaderAuthorization, BearerScheme+" "+strings.TrimSpace(token))
}

func ParseBearer(value string) (string, bool) {
	value = strings.TrimSpace(value)
	if value == "" {
		return "", false
	}
	if len(value) < len(BearerScheme) {
		return "", false
	}
	scheme := value[:len(BearerScheme)]
	if !strings.EqualFold(scheme, BearerScheme) {
		return "", false
	}
	if len(value) == len(BearerScheme) {
		return "", true
	}
	r, _ := utf8.DecodeRuneInString(value[len(BearerScheme):])
	if r == utf8.RuneError {
		return "", false
	}
	if !unicode.IsSpace(r) {
		return "", false
	}
	return strings.TrimSpace(value[len(BearerScheme):]), true
}

func SetOpcode(header http.Header, opcode int) {
	if header == nil {
		return
	}
	header.Set(HeaderOpcode, strconv.Itoa(opcode))
}

func ParseOpcode(value string) (int, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return 0, nil
	}
	return strconv.Atoi(value)
}

// ForwardHeaders retains end-to-end headers, removing connection-specific fields.
func ForwardHeaders(header http.Header) http.Header {
	out := header.Clone()
	for _, value := range header.Values("Connection") {
		for _, name := range strings.Split(value, ",") {
			out.Del(strings.TrimSpace(name))
		}
	}
	for _, name := range []string{"Connection", "Proxy-Connection", "Keep-Alive", "Proxy-Authenticate", "Proxy-Authorization", "Te", "Trailer", "Transfer-Encoding", "Upgrade"} {
		out.Del(name)
	}
	return out
}

// NativeRequestHeaders replaces local Satori authorization at the upstream boundary.
func NativeRequestHeaders(header http.Header) http.Header {
	out := ForwardHeaders(header)
	for _, name := range []string{"Authorization", HeaderPlatform, HeaderSelfID, HeaderSatoriPlatform, HeaderSatoriUserID} {
		out.Del(name)
	}
	return out
}
