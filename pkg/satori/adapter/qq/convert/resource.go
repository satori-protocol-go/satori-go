package convert

import (
	"encoding/base64"
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

var dataURIBase64Pattern = regexp.MustCompile(`^data:([\w/.+-]+);base64,`)

type MessageResourcePayload struct {
	URL      string
	FileData string
}

func ResolveMessageResourcePayload(src string) (MessageResourcePayload, error) {
	src = strings.TrimSpace(src)
	if src == "" {
		return MessageResourcePayload{}, errors.New("resource src is empty")
	}

	if match := dataURIBase64Pattern.FindStringSubmatch(src); len(match) >= 2 {
		encoded := strings.TrimPrefix(src, match[0])
		data, err := base64.StdEncoding.DecodeString(encoded)
		if err != nil {
			if decoded, rawErr := base64.RawStdEncoding.DecodeString(encoded); rawErr == nil {
				data = decoded
			} else {
				return MessageResourcePayload{}, err
			}
		}
		return MessageResourcePayload{FileData: base64.StdEncoding.EncodeToString(data)}, nil
	}

	if strings.HasPrefix(src, "file://") {
		path := strings.TrimPrefix(src, "file://")
		return loadMessageResourceFile(path)
	}

	if strings.HasPrefix(src, "http://") || strings.HasPrefix(src, "https://") || strings.Contains(src, "://") {
		return MessageResourcePayload{URL: src}, nil
	}

	if _, err := os.Stat(src); err == nil {
		return loadMessageResourceFile(src)
	}

	return MessageResourcePayload{URL: src}, nil
}

func DecodeMessageBase64(raw string) ([]byte, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, errors.New("empty base64 payload")
	}
	if data, err := base64.StdEncoding.DecodeString(raw); err == nil {
		return data, nil
	}
	if data, err := base64.RawStdEncoding.DecodeString(raw); err == nil {
		return data, nil
	}
	return base64.URLEncoding.DecodeString(raw)
}

func MapMessageResourceFileType(kind MessageResourceKind) uint64 {
	switch kind {
	case MessageResourceImage:
		return 1
	case MessageResourceVideo:
		return 2
	case MessageResourceAudio:
		return 3
	default:
		return 4
	}
}

func loadMessageResourceFile(path string) (MessageResourcePayload, error) {
	path = filepath.Clean(path)
	data, err := os.ReadFile(path)
	if err != nil {
		return MessageResourcePayload{}, err
	}
	return MessageResourcePayload{FileData: base64.StdEncoding.EncodeToString(data)}, nil
}
