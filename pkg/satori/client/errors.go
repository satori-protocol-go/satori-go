package client

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"

	"github.com/satori-protocol-go/satori-go/pkg/satori/model/message"
)

type SatoriError interface {
	error
	HTTPStatus() int
	ResponseBody() string
}

var (
	ErrBadRequest       = errors.New("satori: bad request")
	ErrUnauthorized     = errors.New("satori: unauthorized")
	ErrForbidden        = errors.New("satori: forbidden")
	ErrNotFound         = errors.New("satori: not found")
	ErrMethodNotAllowed = errors.New("satori: method not allowed")
	ErrRateLimited      = errors.New("satori: rate limited")
	ErrServer           = errors.New("satori: server error")
)

type RequestError struct {
	StatusCode int
	Body       string
	Header     http.Header
	Cause      error
	Operation  string
}

func (e *RequestError) Error() string {
	if e == nil {
		return ""
	}
	if e.Cause != nil {
		return fmt.Sprintf("%s failed while reading HTTP %d response: %v", e.Operation, e.StatusCode, e.Cause)
	}
	if e.Body != "" {
		return fmt.Sprintf("request failed with status %d: %s", e.StatusCode, e.Body)
	}
	return fmt.Sprintf("request failed with status %d", e.StatusCode)
}

func (e *RequestError) HTTPStatus() int {
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

func (e *RequestError) ResponseBody() string {
	if e == nil {
		return ""
	}
	return e.Body
}

func (e *RequestError) Is(target error) bool {
	if e == nil {
		return false
	}

	status := e.HTTPStatus()
	switch target {
	case ErrBadRequest:
		return status == http.StatusBadRequest
	case ErrUnauthorized:
		return status == http.StatusUnauthorized
	case ErrForbidden:
		return status == http.StatusForbidden
	case ErrNotFound:
		return status == http.StatusNotFound
	case ErrMethodNotAllowed:
		return status == http.StatusMethodNotAllowed
	case ErrRateLimited:
		return status == http.StatusTooManyRequests
	case ErrServer:
		return status >= http.StatusInternalServerError
	default:
		return false
	}
}

type BadRequestError struct{ *RequestError }
type UnauthorizedError struct{ *RequestError }
type ForbiddenError struct{ *RequestError }
type NotFoundError struct{ *RequestError }
type MethodNotAllowedError struct{ *RequestError }
type RateLimitedError struct{ *RequestError }
type ServerError struct{ *RequestError }

func NewBadRequestError(body string) *BadRequestError {
	return &BadRequestError{RequestError: newRequestError(http.StatusBadRequest, body)}
}

func NewUnauthorizedError(body string) *UnauthorizedError {
	return &UnauthorizedError{RequestError: newRequestError(http.StatusUnauthorized, body)}
}

func NewForbiddenError(body string) *ForbiddenError {
	return &ForbiddenError{RequestError: newRequestError(http.StatusForbidden, body)}
}

func NewNotFoundError(body string) *NotFoundError {
	return &NotFoundError{RequestError: newRequestError(http.StatusNotFound, body)}
}

func NewMethodNotAllowedError(body string) *MethodNotAllowedError {
	return &MethodNotAllowedError{RequestError: newRequestError(http.StatusMethodNotAllowed, body)}
}

func NewRateLimitedError(body string) *RateLimitedError {
	return &RateLimitedError{RequestError: newRequestError(http.StatusTooManyRequests, body)}
}

func NewServerError(statusCode int, body string) *ServerError {
	if statusCode < http.StatusInternalServerError {
		statusCode = http.StatusInternalServerError
	}
	return &ServerError{RequestError: newRequestError(statusCode, body)}
}

func StatusCode(err error) int {
	var sErr SatoriError
	if errors.As(err, &sErr) {
		return sErr.HTTPStatus()
	}
	return 0
}

func IsStatus(err error, statusCode int) bool {
	if err == nil {
		return false
	}
	return StatusCode(err) == statusCode
}

func errorFromStatusCode(statusCode int, payload []byte, headers ...http.Header) error {
	base := newRequestError(statusCode, string(payload))
	if len(headers) > 0 {
		base.Header = headers[0].Clone()
	}
	switch statusCode {
	case http.StatusBadRequest:
		return &BadRequestError{base}
	case http.StatusUnauthorized:
		return &UnauthorizedError{base}
	case http.StatusForbidden:
		return &ForbiddenError{base}
	case http.StatusNotFound:
		return &NotFoundError{base}
	case http.StatusMethodNotAllowed:
		return &MethodNotAllowedError{base}
	case http.StatusTooManyRequests:
		return &RateLimitedError{base}
	default:
		if statusCode >= 500 {
			return &ServerError{base}
		}
		return base
	}
}

func (e *RequestError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Cause
}
func (e *RequestError) ResponseHeaders() http.Header {
	if e == nil {
		return nil
	}
	return e.Header.Clone()
}

// PartialMessages returns only server-confirmed messages, never fabricated or retried segments.
func (e *RequestError) PartialMessages() ([]*message.Message, error) {
	if e == nil || e.Cause != nil || !json.Valid([]byte(e.Body)) {
		return nil, nil
	}
	var result struct {
		Messages []*message.Message `json:"messages"`
	}
	if err := json.Unmarshal([]byte(e.Body), &result); err != nil {
		return nil, err
	}
	return result.Messages, nil
}

func newRequestError(statusCode int, body string) *RequestError {
	return &RequestError{
		StatusCode: statusCode,
		Body:       body,
	}
}
