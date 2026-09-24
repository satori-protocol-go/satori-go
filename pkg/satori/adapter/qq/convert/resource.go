package convert

import (
	"encoding/base64"
	"errors"
	"mime"
	"net/url"
	"path"
	"strings"
)

// MessageResourcePayload describes a resource without performing filesystem I/O.
// Internal URLs must be resolved by the owning Satori server, not by the parser.
type MessageResourcePayload struct {
	URL         string
	Internal    string
	Data        []byte
	FileName    string
	ContentType string
}

func ResolveMessageResourcePayload(src string) (MessageResourcePayload, error) {
	result := MessageResourcePayload{FileName: "upload"}
	if strings.HasPrefix(src, "data:") {
		metadata, data, ok := strings.Cut(strings.TrimPrefix(src, "data:"), ",")
		if !ok {
			return result, errors.New("invalid data URI")
		}
		encoded := strings.HasSuffix(metadata, ";base64")
		metadata = strings.TrimSuffix(metadata, ";base64")
		if metadata == "" {
			metadata = "text/plain"
		}
		contentType, _, err := mime.ParseMediaType(metadata)
		if err != nil {
			return result, err
		}
		result.ContentType = contentType
		if encoded {
			result.Data, err = base64.StdEncoding.DecodeString(data)
			if err != nil {
				result.Data, err = base64.RawStdEncoding.DecodeString(data)
			}
		} else {
			var decoded string
			decoded, err = url.PathUnescape(data)
			result.Data = []byte(decoded)
		}
		if err != nil {
			return result, err
		}
		if len(result.Data) == 0 {
			return result, errors.New("resource data is empty")
		}
		if extensions, _ := mime.ExtensionsByType(contentType); len(extensions) > 0 {
			result.FileName += extensions[0]
		}
		return result, nil
	}
	if strings.HasPrefix(src, "internal:") {
		result.Internal = src
		result.FileName = path.Base(src)
		return result, nil
	}
	parsed, err := url.Parse(src)
	if err != nil || parsed.Host == "" || parsed.User != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") {
		return result, errors.New("resource must be an HTTP URL, data URI, or an owned internal URL")
	}
	result.URL = src
	if name := path.Base(parsed.Path); name != "" && name != "." && name != "/" {
		result.FileName = name
	}
	return result, nil
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
