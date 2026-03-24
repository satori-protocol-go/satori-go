package client

import (
	"errors"
	"fmt"
	"net/http"
	"strings"
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
}

func (e *RequestError) Error() string {
	if e == nil {
		return ""
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

	switch target {
	case ErrBadRequest:
		return e.StatusCode == http.StatusBadRequest
	case ErrUnauthorized:
		return e.StatusCode == http.StatusUnauthorized
	case ErrForbidden:
		return e.StatusCode == http.StatusForbidden
	case ErrNotFound:
		return e.StatusCode == http.StatusNotFound
	case ErrMethodNotAllowed:
		return e.StatusCode == http.StatusMethodNotAllowed
	case ErrRateLimited:
		return e.StatusCode == http.StatusTooManyRequests
	case ErrServer:
		return e.StatusCode >= http.StatusInternalServerError
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

func errorFromStatusCode(statusCode int, payload []byte) error {
	body := strings.TrimSpace(string(payload))
	switch statusCode {
	case http.StatusBadRequest:
		return NewBadRequestError(body)
	case http.StatusUnauthorized:
		return NewUnauthorizedError(body)
	case http.StatusForbidden:
		return NewForbiddenError(body)
	case http.StatusNotFound:
		return NewNotFoundError(body)
	case http.StatusMethodNotAllowed:
		return NewMethodNotAllowedError(body)
	case http.StatusTooManyRequests:
		return NewRateLimitedError(body)
	default:
		if statusCode >= http.StatusInternalServerError {
			return NewServerError(statusCode, body)
		}
		return newRequestError(statusCode, body)
	}
}

func newRequestError(statusCode int, body string) *RequestError {
	return &RequestError{
		StatusCode: statusCode,
		Body:       strings.TrimSpace(body),
	}
}
