package server_test

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/satori-protocol-go/satori-go/pkg/satori/protocol"
	satoriserver "github.com/satori-protocol-go/satori-go/pkg/satori/server"
)

func makeServer(t *testing.T, cfg satoriserver.Config) *satoriserver.Server {
	t.Helper()
	srv, err := satoriserver.NewServer(cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := srv.Close(); err != nil {
			t.Error(err)
		}
	})
	return srv
}
func serverHandler(t *testing.T, srv *satoriserver.Server) http.Handler {
	t.Helper()
	h, err := srv.Handler()
	if err != nil {
		t.Fatal(err)
	}
	return h
}
func request(h http.Handler, method, path, body, token string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, path, strings.NewReader(body))
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("Authorization", token)
	protocol.SetIdentityHeaders(r.Header, "mock", "bot")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}

func TestServerRouting(t *testing.T) {
	for _, mode := range []string{"default", "external-config", "external-setter", "subpath"} {
		t.Run(mode, func(t *testing.T) {
			parent := chi.NewRouter()
			parent.Get("/custom", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(202) })
			cfg := satoriserver.Config{Host: "0.0.0.0", BaseHandler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(418)
				io.WriteString(w, r.Method+" "+r.URL.Path)
			})}
			if mode == "external-config" {
				cfg.ReplaceRouter = parent
			}
			srv := makeServer(t, cfg)
			if mode == "external-setter" {
				srv.ReplaceRouter(parent)
			}
			srv.RouteInternal("*", func(r *satoriserver.Request[satoriserver.InternalParam]) (any, error) {
				data, err := io.ReadAll(r.Origin.Body)
				return satoriserver.NewResponse(207, data), err
			})
			if err := srv.Handle("/healthz", http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(204) })); err != nil {
				t.Fatal(err)
			}
			if err := srv.Method("POST", "/only-post", http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(202) })); err != nil {
				t.Fatal(err)
			}
			var h http.Handler
			prefix := ""
			if mode == "subpath" {
				prefix = "/satori"
				parent.Route(prefix, func(r chi.Router) {
					if err := srv.RegisterRoutes(r); err != nil {
						t.Fatal(err)
					}
				})
				h = parent
			} else {
				h = serverHandler(t, srv)
			}
			for _, tc := range []struct {
				method, path, body string
				status             int
			}{{"POST", "/v1/meta", "{}", 200}, {"PATCH", "/v1/internal/echo", "native", 207}, {"GET", "/healthz", "", 204}, {"POST", "/only-post", "", 202}} {
				w := request(h, tc.method, prefix+tc.path, tc.body, "")
				if w.Code != tc.status || (tc.status == 207 && w.Body.String() != "native") {
					t.Fatalf("%s %s=%d %s", tc.method, tc.path, w.Code, w.Body)
				}
			}
			if mode != "subpath" {
				for _, path := range []string{"/outside", "/only-post"} {
					w := request(h, "GET", path, "", "")
					if w.Code != 418 || w.Body.String() != "GET "+path {
						t.Fatalf("fallback=%d %s", w.Code, w.Body)
					}
				}
			}
			if mode != "default" {
				if w := request(h, "GET", "/custom", "", ""); w.Code != 202 {
					t.Fatalf("custom=%d", w.Code)
				}
			}
		})
	}
}
