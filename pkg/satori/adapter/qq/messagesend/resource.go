package messagesend

import (
	"encoding/base64"
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/satori-protocol-go/satori-go/pkg/satori/adapter/qq/messagecodec"
)

var dataURIBase64Pattern = regexp.MustCompile(`^data:([\w/.+-]+);base64,`)

type resourcePayload struct {
	URL      string
	FileData string
}

func resolveResourcePayload(src string) (resourcePayload, error) {
	src = strings.TrimSpace(src)
	if src == "" {
		return resourcePayload{}, errors.New("resource src is empty")
	}

	if match := dataURIBase64Pattern.FindStringSubmatch(src); len(match) >= 2 {
		encoded := strings.TrimPrefix(src, match[0])
		data, err := base64.StdEncoding.DecodeString(encoded)
		if err != nil {
			if decoded, rawErr := base64.RawStdEncoding.DecodeString(encoded); rawErr == nil {
				data = decoded
			} else {
				return resourcePayload{}, err
			}
		}
		return resourcePayload{
			FileData: base64.StdEncoding.EncodeToString(data),
		}, nil
	}

	if strings.HasPrefix(src, "file://") {
		path := strings.TrimPrefix(src, "file://")
		return loadFileAsPayload(path)
	}

	if strings.HasPrefix(src, "http://") || strings.HasPrefix(src, "https://") {
		return resourcePayload{URL: src}, nil
	}

	if strings.Contains(src, "://") {
		return resourcePayload{URL: src}, nil
	}

	if _, err := os.Stat(src); err == nil {
		return loadFileAsPayload(src)
	}

	return resourcePayload{URL: src}, nil
}

func loadFileAsPayload(path string) (resourcePayload, error) {
	path = filepath.Clean(path)
	data, err := os.ReadFile(path)
	if err != nil {
		return resourcePayload{}, err
	}
	return resourcePayload{
		FileData: base64.StdEncoding.EncodeToString(data),
	}, nil
}

func decodeBase64Data(raw string) ([]byte, error) {
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

func resourceFileType(kind messagecodec.ResourceKind) uint64 {
	switch kind {
	case messagecodec.ResourceImage:
		return 1
	case messagecodec.ResourceVideo:
		return 2
	case messagecodec.ResourceAudio:
		return 3
	default:
		return 4
	}
}
