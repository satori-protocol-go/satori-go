package network

import (
	"io"
	"net/http"
	"strings"

	"github.com/satori-protocol-go/satori-go/pkg/satori/protocol"
)

func decodeJSON(payload []byte, target any) error {
	_, err := protocol.DecodeJSONBytes(payload, target)
	return err
}

func readResponseBody(resp *http.Response) ([]byte, error) {
	if resp == nil || resp.Body == nil {
		return []byte{}, nil
	}
	defer resp.Body.Close()
	return io.ReadAll(resp.Body)
}

func validateHTTPStatus(statusCode int, payload []byte) error {
	if statusCode >= 200 && statusCode < 300 {
		return nil
	}
	return newHTTPStatusError(statusCode, payload)
}

type HTTPStatusError struct {
	StatusCode int
	Body       string
}

func (e *HTTPStatusError) Error() string {
	if e == nil {
		return ""
	}
	if e.Body == "" {
		return "request failed"
	}
	return e.Body
}

func newHTTPStatusError(statusCode int, payload []byte) error {
	body := strings.TrimSpace(string(payload))
	if body == "" {
		body = http.StatusText(statusCode)
		if body == "" {
			body = "request failed"
		}
	}
	return &HTTPStatusError{
		StatusCode: statusCode,
		Body:       body,
	}
}
