package server

import (
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
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

// UploadFile is one Satori upload part. Filename is optional; Name is the map key.
// Parts are bounded by the server's existing MaxRequestBytes HTTP boundary.
type UploadFile struct {
	Filename    string
	ContentType string
	Data        []byte
}

func parseUploads(request *http.Request) (UploadCreateParam, error) {
	reader, err := request.MultipartReader()
	if err != nil {
		return nil, BadRequest(err.Error())
	}
	files := UploadCreateParam{}
	for {
		part, err := reader.NextPart()
		if err == io.EOF {
			return files, nil
		}
		if err != nil {
			return nil, uploadReadError(err)
		}
		attrs, err := ParseContentDisposition(part.Header.Get("Content-Disposition"))
		if err != nil {
			part.Close()
			return nil, BadRequest("invalid upload disposition")
		}
		name := attrs["name"]
		if _, exists := files[name]; name == "" || exists {
			part.Close()
			return nil, BadRequest("upload names must be present and unique")
		}
		contentType := part.Header.Get("Content-Type")
		if _, _, err := mime.ParseMediaType(contentType); err != nil {
			part.Close()
			return nil, BadRequest("upload content type is required")
		}
		data, err := io.ReadAll(part)
		part.Close()
		if err != nil {
			return nil, uploadReadError(err)
		}
		files[name] = UploadFile{Filename: attrs["filename"], ContentType: contentType, Data: data}
	}
}
func uploadReadError(err error) error {
	var limit *http.MaxBytesError
	if errors.As(err, &limit) {
		return NewActionError(413, "request exceeds local limit", err)
	}
	return BadRequest(err.Error())
}
