package action

import (
	satoriserver "github.com/satori-protocol-go/satori-go/pkg/satori/server"
)

func (h *Handler) handleLoginGet(request satoriserver.Request[satoriserver.LoginGetParam]) (any, error) {
	if h.ensureLogins != nil {
		if err := h.ensureLogins(requestContext(request.Origin)); err != nil {
			return nil, err
		}
	}
	if h.findLogin == nil {
		return nil, satoriserver.NotFound("login not found")
	}
	login := h.findLogin(request.Platform, request.SelfID)
	if login == nil {
		return nil, satoriserver.NotFound("login not found")
	}
	return login, nil
}
