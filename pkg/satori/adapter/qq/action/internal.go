package action

import (
	"strings"

	satoriserver "github.com/satori-protocol-go/satori-go/pkg/satori/server"
)

func (h *Handler) handleInternalRoute(request satoriserver.Request[map[string]any]) (any, error) {
	if h.handleInternal == nil {
		return nil, satoriserver.NotFound("internal route is not configured")
	}
	path := strings.TrimPrefix(request.Action, "internal/")
	params := request.Params
	if params == nil {
		params = map[string]any{}
	}

	resp, err := h.handleInternal(satoriserver.Request[map[string]any]{
		Origin:   request.Origin,
		Action:   request.Action,
		Params:   params,
		Platform: request.Platform,
		SelfID:   request.SelfID,
	}, path)
	if err != nil {
		return nil, err
	}
	if resp == nil {
		return map[string]any{}, nil
	}
	return resp, nil
}
