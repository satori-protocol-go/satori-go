package server

import (
	"errors"
	"fmt"
	"net/http"
	"strings"
)

type rootRoute struct {
	method  string
	pattern string
	handler http.Handler
}

var validHTTPMethods = map[string]struct{}{
	http.MethodGet:     {},
	http.MethodHead:    {},
	http.MethodPost:    {},
	http.MethodPut:     {},
	http.MethodPatch:   {},
	http.MethodDelete:  {},
	http.MethodConnect: {},
	http.MethodOptions: {},
	http.MethodTrace:   {},
}

func (s *Server) Handle(pattern string, handler http.Handler) error {
	return s.Method("", pattern, handler)
}

func (s *Server) HandleFunc(pattern string, handler http.HandlerFunc) error {
	if handler == nil {
		return errors.New("handler cannot be nil")
	}
	return s.Handle(pattern, handler)
}

func (s *Server) Methods(pattern string, handler http.Handler, methods ...string) error {
	if len(methods) == 0 {
		return s.Handle(pattern, handler)
	}
	for _, method := range methods {
		if err := s.Method(method, pattern, handler); err != nil {
			return err
		}
	}
	return nil
}

func (s *Server) Method(method string, pattern string, handler http.Handler) error {
	if handler == nil {
		return errors.New("handler cannot be nil")
	}

	normalizedPattern, err := normalizeRootRoutePattern(pattern)
	if err != nil {
		return err
	}
	normalizedMethod, err := normalizeRootRouteMethod(method)
	if err != nil {
		return err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	for _, route := range s.rootRoutes {
		if route.pattern != normalizedPattern {
			continue
		}
		if route.method == normalizedMethod || route.method == "" || normalizedMethod == "" {
			conflictMethod := normalizedMethod
			if conflictMethod == "" {
				conflictMethod = "ANY"
			}
			return fmt.Errorf("root route already registered: %s %s", conflictMethod, normalizedPattern)
		}
	}

	s.rootRoutes = append(s.rootRoutes, rootRoute{
		method:  normalizedMethod,
		pattern: normalizedPattern,
		handler: handler,
	})

	return nil
}

func normalizeRootRoutePattern(pattern string) (string, error) {
	pattern = strings.TrimSpace(pattern)
	if pattern == "" {
		return "", errors.New("pattern cannot be empty")
	}
	if !strings.HasPrefix(pattern, "/") {
		pattern = "/" + pattern
	}
	return pattern, nil
}

func normalizeRootRouteMethod(method string) (string, error) {
	method = strings.ToUpper(strings.TrimSpace(method))
	if method == "" {
		return "", nil
	}
	if _, ok := validHTTPMethods[method]; !ok {
		return "", fmt.Errorf("invalid method %q", method)
	}
	return method, nil
}
