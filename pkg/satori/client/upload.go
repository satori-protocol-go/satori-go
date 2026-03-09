package client

import (
	"io"
	"mime"
	"os"
	"path/filepath"
)

type Upload struct {
	Value       []byte
	Filename    string
	ContentType string
}

func NewUpload(value []byte, filename string, contentType string) Upload {
	copied := make([]byte, len(value))
	copy(copied, value)
	return Upload{Value: copied, Filename: filename, ContentType: contentType}
}

func NewUploadFromReader(reader io.Reader, filename string, contentType string) (Upload, error) {
	data, err := io.ReadAll(reader)
	if err != nil {
		return Upload{}, err
	}
	return NewUpload(data, filename, contentType), nil
}

func NewUploadFromFile(filePath string) (Upload, error) {
	data, err := os.ReadFile(filePath)
	if err != nil {
		return Upload{}, err
	}
	filename := filepath.Base(filePath)
	contentType := mime.TypeByExtension(filepath.Ext(filePath))
	if contentType == "" {
		contentType = "application/octet-stream"
	}
	return Upload{Value: data, Filename: filename, ContentType: contentType}, nil
}

func (u Upload) normalized(fieldName string) Upload {
	if u.ContentType == "" {
		u.ContentType = "application/octet-stream"
	}
	if u.Filename == "" {
		u.Filename = fieldName
	}
	if len(u.Value) == 0 {
		u.Value = []byte{}
	}
	return u
}
