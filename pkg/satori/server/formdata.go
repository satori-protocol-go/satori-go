package server

import (
	"fmt"
	"mime"
	"strings"
)

func ParseContentDisposition(headerValue string) (map[string]string, error) {
	mediaType, params, err := mime.ParseMediaType(headerValue)
	if err != nil {
		return nil, err
	}
	if !strings.EqualFold(mediaType, "form-data") {
		return nil, fmt.Errorf("unsupported content-disposition: %q", headerValue)
	}
	return params, nil
}
