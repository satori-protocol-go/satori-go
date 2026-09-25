package qq

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"strconv"

	"github.com/WindowsSov8forUs/botgo-plus/errs"
	"github.com/WindowsSov8forUs/botgo-plus/media"
	native "github.com/WindowsSov8forUs/botgo-plus/openapi/v1"
	"github.com/satori-protocol-go/satori-go/pkg/satori/logging"
	"github.com/satori-protocol-go/satori-go/pkg/satori/server"
)

// nativeResponseError retains request-local evidence when decoding or validation fails.
// The raw response remains accessible through Meta, but is not part of Error().
type nativeResponseError struct {
	Meta  *native.ResponseMeta
	Cause error
}

func (e *nativeResponseError) Error() string {
	text := fmt.Sprintf("invalid QQ response (upstream HTTP %d)", e.Meta.StatusCode)
	if e.Meta.TraceID != "" {
		text += ", traceID: " + logging.SafeText(e.Meta.TraceID)
	}
	return text + ": " + logging.ErrorText(e.Cause)
}
func (e *nativeResponseError) Unwrap() error { return e.Cause }
func (e *nativeResponseError) HTTPStatus() int {
	if errors.Is(e.Cause, context.Canceled) {
		return 503
	}
	var timeout net.Error
	if errors.Is(e.Cause, context.DeadlineExceeded) || (errors.As(e.Cause, &timeout) && timeout.Timeout()) {
		return 504
	}
	return 502
}
func (e *nativeResponseError) ResponseHeaders() http.Header {
	h := e.Meta.Header.Clone()
	if h == nil {
		h = make(http.Header)
	}
	if e.Meta.TraceID != "" {
		h.Set("X-Tps-trace-ID", e.Meta.TraceID)
	}
	return h
}
func wrapQQResponse(meta *native.ResponseMeta, cause error) error {
	if cause == nil || meta == nil {
		return cause
	}
	var api *errs.APIError
	var classified interface{ HTTPStatus() int }
	if errors.As(cause, &api) || errors.As(cause, &classified) {
		return cause
	}
	return &nativeResponseError{Meta: meta, Cause: cause}
}
func qqErrorHeaders(cause error) http.Header {
	h := server.ErrorHeaders(cause)
	var api *errs.APIError
	if errors.As(cause, &api) {
		if api.TraceID != "" {
			h.Set("X-Tps-trace-ID", api.TraceID)
		}
		if api.ErrorCode != 0 {
			h.Set("X-QQ-Error-Code", strconv.Itoa(api.ErrorCode))
		}
		if value := api.Header.Get("Retry-After"); value != "" {
			h.Set("Retry-After", value)
		}
	}
	var upload *media.UploadError
	if errors.As(cause, &upload) {
		h.Set("X-QQ-Upload-Stage", upload.Stage)
		if upload.PartIndex >= 0 {
			h.Set("X-QQ-Upload-Part", strconv.Itoa(upload.PartIndex))
		}
	}
	// Apply the same allowlist to native values before attaching them to a response.
	holder := &server.ActionError{Header: h}
	return server.ErrorHeaders(holder)
}
