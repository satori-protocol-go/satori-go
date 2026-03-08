package server

import "strings"

type RouterMixin struct {
	routes map[string]RouteCall
}

func (r *RouterMixin) Route(action string, handler RouteCall) string {
	if r.routes == nil {
		r.routes = map[string]RouteCall{}
	}
	normalized := normalizeRouteAction(action)
	if normalized == "" || handler == nil {
		return ""
	}
	r.routes[normalized] = handler
	return normalized
}

func (r *RouterMixin) Routes() map[string]RouteCall {
	if r.routes == nil {
		r.routes = map[string]RouteCall{}
	}
	return r.routes
}

func normalizeRouteAction(action string) string {
	action = strings.TrimSpace(action)
	action = strings.TrimPrefix(action, "/")
	if action == "" {
		return ""
	}
	if _, ok := apiActionSet[action]; ok {
		return action
	}
	if strings.HasPrefix(action, "internal/") {
		return action
	}
	return "internal/" + action
}

func matchRoute(routes map[string]RouteCall, action string) (RouteCall, bool) {
	if len(routes) == 0 {
		return nil, false
	}
	if handler, ok := routes[action]; ok {
		return handler, true
	}
	if strings.HasPrefix(action, "internal") {
		handler, ok := routes["internal/*"]
		return handler, ok
	}
	return nil, false
}
