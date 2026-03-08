package server

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/satori-protocol-go/satori-go/pkg/satori/model/event"
	"github.com/satori-protocol-go/satori-go/pkg/satori/model/login"
)

type Request[T any] struct {
	Origin   *http.Request
	Action   string
	Params   T
	Platform string
	SelfID   string
}

type RouteCall func(request Request[any]) (any, error)

type Router interface {
	Routes() map[string]RouteCall
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
}

type ServerAware interface {
	EnsureServer(server *Server)
}

type RootRoute struct {
	Path    string
	Methods []string
	Handler http.Handler
}

type RootRouteProvider interface {
	RootRoutes() []RootRoute
}

type EventPublisher interface {
	Publisher(ctx context.Context) <-chan *event.Event
}

type WebhookEndpoint struct {
	URL     string
	Token   string
	Timeout time.Duration
}

type Response struct {
	StatusCode int
	Header     http.Header
	Body       []byte
}

func NewResponse(statusCode int, body []byte) *Response {
	return &Response{
		StatusCode: statusCode,
		Header:     http.Header{},
		Body:       body,
	}
}

func (r *Response) statusCodeOrDefault() int {
	if r == nil || r.StatusCode == 0 {
		return http.StatusOK
	}
	return r.StatusCode
}

type ActionFailed struct {
	Code    int
	Message string
	Err     error
}

func (e *ActionFailed) Error() string {
	if e == nil {
		return ""
	}
	if e.Message != "" {
		return e.Message
	}
	if e.Err != nil {
		return e.Err.Error()
	}
	if e.Code > 0 {
		return http.StatusText(e.Code)
	}
	return "action failed"
}

func (e *ActionFailed) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Err
}

func NewActionFailed(code int, message string, err error) *ActionFailed {
	if code == 0 {
		code = http.StatusBadRequest
	}
	return &ActionFailed{
		Code:    code,
		Message: message,
		Err:     err,
	}
}

func actionFailedf(code int, format string, args ...any) error {
	return NewActionFailed(code, fmt.Sprintf(format, args...), nil)
}

func BadRequest(message string) error {
	return NewActionFailed(http.StatusBadRequest, message, nil)
}

func Unauthorized(message string) error {
	return NewActionFailed(http.StatusUnauthorized, message, nil)
}

func Forbidden(message string) error {
	return NewActionFailed(http.StatusForbidden, message, nil)
}

func NotFound(message string) error {
	return NewActionFailed(http.StatusNotFound, message, nil)
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
		var actionErr *ActionFailed
		if errors.As(err, &actionErr) && actionErr.Code != 0 {
			return actionErr.Code
		}
		return http.StatusInternalServerError
	}
}
