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
	platform := strings.TrimSpace(header.Get(HeaderPlatform))
	if platform == "" {
		platform = strings.TrimSpace(header.Get(HeaderSatoriPlatform))
	}
	if platform == "" {
		return "", "", ErrMissingPlatformHeader
	}

	selfID := strings.TrimSpace(header.Get(HeaderSelfID))
	if selfID == "" {
		selfID = strings.TrimSpace(header.Get(HeaderSatoriUserID))
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
