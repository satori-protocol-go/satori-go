package network

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
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
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		if len(data) > 64*1024 {
			data = data[:64*1024]
		}
		return nil, &HTTPStatusError{StatusCode: resp.StatusCode, Body: string(data), Header: resp.Header.Clone(), Cause: err}
	}
	return data, nil
}

func validateHTTPStatus(statusCode int, payload []byte, headers ...http.Header) error {
	if statusCode >= 200 && statusCode < 300 {
		return nil
	}
	err := newHTTPStatusError(statusCode, payload)
	if len(headers) > 0 {
		err.(*HTTPStatusError).Header = headers[0].Clone()
	}
	return err
}

type HTTPStatusError struct {
	StatusCode int
	Body       string
	Header     http.Header
	Cause      error
}

func (e *HTTPStatusError) Error() string {
	if e == nil {
		return ""
	}
	if e.Cause != nil {
		return fmt.Sprintf("read HTTP %d response: %v", e.StatusCode, e.Cause)
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

func (e *HTTPStatusError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Cause
}
func (e *HTTPStatusError) HTTPStatus() int {
	if e == nil {
		return 0
	}
	if e.Cause != nil {
		if errors.Is(e.Cause, context.Canceled) {
			return http.StatusServiceUnavailable
		}
		if errors.Is(e.Cause, context.DeadlineExceeded) {
			return http.StatusGatewayTimeout
		}
		var timeout net.Error
		if errors.As(e.Cause, &timeout) && timeout.Timeout() {
			return http.StatusGatewayTimeout
		}
		return http.StatusBadGateway
	}
	return e.StatusCode
}
func (e *HTTPStatusError) ResponseBody() string {
	if e == nil {
		return ""
	}
	return e.Body
}
func (e *HTTPStatusError) ResponseHeaders() http.Header {
	if e == nil {
		return nil
	}
	return e.Header.Clone()
}
