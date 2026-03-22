package server

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/satori-protocol-go/satori-go/pkg/satori/model/event"
	"github.com/satori-protocol-go/satori-go/pkg/satori/model/login"
)

type Router interface {
	Routes() map[string]RouteCall[any, any]
}

type Provider interface {
	GetLogins(ctx context.Context) ([]*login.Login, error)
	ProxyUrls() []string
	Ensure(platform string, selfID string) bool
	HandleInternal(request Request[map[string]any], path string) (*Response, error)
	HandleProxied(prefix string, rawURL string) (*Response, error)
}

type Adapter interface {
	Provider
	Router
	EnsureServer(server *Server)
}

type RootRouteRegistrar interface {
	RegisterRootRoutes(router chi.Router)
}

type EventPublisher interface {
	Publisher(ctx context.Context) <-chan *event.Event
}

type Preparable interface {
	Prepare(ctx context.Context) error
}

type Blockable interface {
	Block(ctx context.Context) error
}

type Cleanable interface {
	Cleanup(ctx context.Context) error
}

type WebhookEndpoint struct {
	URL     string
	Token   string
	Timeout time.Duration
}

type Response struct {
	StatusCode    int
	Header        http.Header
	Body          []byte
	Stream        io.ReadCloser
	ContentLength int64
}

func NewResponse(statusCode int, body []byte) *Response {
	return &Response{
		StatusCode:    statusCode,
		Header:        http.Header{},
		Body:          body,
		ContentLength: int64(len(body)),
	}
}

func NewStreamResponse(statusCode int, stream io.ReadCloser) *Response {
	return &Response{
		StatusCode:    statusCode,
		Header:        http.Header{},
		Stream:        stream,
		ContentLength: -1,
	}
}

func (r *Response) statusCodeOrDefault() int {
	if r == nil || r.StatusCode == 0 {
		return http.StatusOK
	}
	return r.StatusCode
}

func (r *Response) closeStream() {
	if r == nil || r.Stream == nil {
		return
	}
	_ = r.Stream.Close()
	r.Stream = nil
}

type SatoriError interface {
	error
	HTTPStatus() int
}

type ActionError struct {
	Status  int
	Message string
	Err     error
}

func (e *ActionError) Error() string {
	if e == nil {
		return ""
	}
	if e.Message != "" {
		return e.Message
	}
	if e.Err != nil {
		return e.Err.Error()
	}
	if e.Status > 0 {
		return http.StatusText(e.Status)
	}
	return "action failed"
}

func (e *ActionError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Err
}

func (e *ActionError) HTTPStatus() int {
	if e == nil || e.Status <= 0 {
		return http.StatusInternalServerError
	}
	return e.Status
}

func NewActionError(status int, message string, err error) *ActionError {
	if status == 0 {
		status = http.StatusBadRequest
	}
	return &ActionError{
		Status:  status,
		Message: message,
		Err:     err,
	}
}

func BadRequest(message string) error {
	return NewActionError(http.StatusBadRequest, message, nil)
}

func Unauthorized(message string) error {
	return NewActionError(http.StatusUnauthorized, message, nil)
}

func Forbidden(message string) error {
	return NewActionError(http.StatusForbidden, message, nil)
}

func NotFound(message string) error {
	return NewActionError(http.StatusNotFound, message, nil)
}

func statusFromError(err error) int {
	if err == nil {
		return http.StatusOK
	}
	switch {
	case errors.Is(err, context.Canceled):
		return http.StatusServiceUnavailable
	case errors.Is(err, context.DeadlineExceeded):
		return http.StatusGatewayTimeout
	default:
		var sErr SatoriError
		if errors.As(err, &sErr) {
			return sErr.HTTPStatus()
		}
		if errors.Is(err, os.ErrNotExist) {
			return http.StatusNotFound
		}
		if errors.Is(err, os.ErrPermission) {
			return http.StatusForbidden
		}
		var syntaxErr *json.SyntaxError
		if errors.As(err, &syntaxErr) {
			return http.StatusBadRequest
		}
		var unmarshalTypeErr *json.UnmarshalTypeError
		if errors.As(err, &unmarshalTypeErr) {
			return http.StatusBadRequest
		}
		var urlErr *url.Error
		if errors.As(err, &urlErr) && urlErr.Timeout() {
			return http.StatusGatewayTimeout
		}
		var netErr net.Error
		if errors.As(err, &netErr) && netErr.Timeout() {
			return http.StatusGatewayTimeout
		}
		return http.StatusInternalServerError
	}
}
