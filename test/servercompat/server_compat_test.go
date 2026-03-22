package servercompat

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	modellogin "github.com/satori-protocol-go/satori-go/pkg/satori/model/login"
	satoriserver "github.com/satori-protocol-go/satori-go/pkg/satori/server"
)

type failingLoginProvider struct {
	proxyURLs []string
	loginErr  error
}

func (p *failingLoginProvider) GetLogins(context.Context) ([]*modellogin.Login, error) {
	if p.loginErr != nil {
		return nil, p.loginErr
	}
	return []*modellogin.Login{}, nil
}

func (p *failingLoginProvider) ProxyUrls() []string {
	return append([]string(nil), p.proxyURLs...)
}

func (p *failingLoginProvider) Ensure(string, string) bool {
	return true
}

func (p *failingLoginProvider) HandleInternal(
	satoriserver.Request[map[string]any],
	string,
) (*satoriserver.Response, error) {
	return nil, nil
}

func (p *failingLoginProvider) HandleProxied(string, string) (*satoriserver.Response, error) {
	return nil, nil
}

func TestBaseHandlerFallbackForNotFoundAndMethodNotAllowed(t *testing.T) {
	base := http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		w.WriteHeader(http.StatusTeapot)
		_, _ = w.Write([]byte(request.Method + " " + request.URL.Path))
	})

	srv, err := satoriserver.NewServer(satoriserver.Config{BaseHandler: base})
	if err != nil {
		t.Fatalf("new server failed: %v", err)
	}
	defer srv.Close()
	if err := srv.Method(http.MethodPost, "/only-post", http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})); err != nil {
		t.Fatalf("register method route failed: %v", err)
	}

	handler, err := srv.Handler()
	if err != nil {
		t.Fatalf("build handler failed: %v", err)
	}
	httpServer := httptest.NewServer(handler)
	defer httpServer.Close()

	notFoundResp, err := http.Get(httpServer.URL + "/outside")
	if err != nil {
		t.Fatalf("not found request failed: %v", err)
	}
	defer notFoundResp.Body.Close()
	if notFoundResp.StatusCode != http.StatusTeapot {
		body, _ := io.ReadAll(notFoundResp.Body)
		t.Fatalf("not found status mismatch: got %d body=%s", notFoundResp.StatusCode, string(body))
	}

	methodResp, err := http.Get(httpServer.URL + "/only-post")
	if err != nil {
		t.Fatalf("method mismatch request failed: %v", err)
	}
	defer methodResp.Body.Close()
	if methodResp.StatusCode != http.StatusTeapot {
		body, _ := io.ReadAll(methodResp.Body)
		t.Fatalf("method mismatch status mismatch: got %d body=%s", methodResp.StatusCode, string(body))
	}
}

func TestReplaceRouterBuildsProtocolRoutesOnProvidedRouter(t *testing.T) {
	parent := chi.NewRouter()
	parent.Get("/custom", func(w http.ResponseWriter, request *http.Request) {
		w.WriteHeader(http.StatusAccepted)
		_, _ = w.Write([]byte("custom"))
	})

	srv, err := satoriserver.NewServer(satoriserver.Config{ReplaceRouter: parent})
	if err != nil {
		t.Fatalf("new server failed: %v", err)
	}
	defer srv.Close()

	srv.RouteInternal("*", func(request *satoriserver.Request[satoriserver.InternalParam]) (any, error) {
		return map[string]any{"action": request.Action}, nil
	})

	handler, err := srv.Handler()
	if err != nil {
		t.Fatalf("build handler failed: %v", err)
	}
	httpServer := httptest.NewServer(handler)
	defer httpServer.Close()

	customResp, err := http.Get(httpServer.URL + "/custom")
	if err != nil {
		t.Fatalf("custom request failed: %v", err)
	}
	defer customResp.Body.Close()
	if customResp.StatusCode != http.StatusAccepted {
		body, _ := io.ReadAll(customResp.Body)
		t.Fatalf("custom status mismatch: got %d body=%s", customResp.StatusCode, string(body))
	}

	req, err := http.NewRequest(http.MethodPost, httpServer.URL+"/v1/internal/ping", strings.NewReader(`{}`))
	if err != nil {
		t.Fatalf("new request failed: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Platform", "mock")
	req.Header.Set("X-Self-ID", "bot")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("status mismatch: got %d body=%s", resp.StatusCode, string(body))
	}
}

func TestReplaceRouterSetterBuildsProtocolRoutes(t *testing.T) {
	parent := chi.NewRouter()
	parent.Get("/custom-setter", func(w http.ResponseWriter, request *http.Request) {
		w.WriteHeader(http.StatusAccepted)
		_, _ = w.Write([]byte("custom-setter"))
	})

	srv, err := satoriserver.NewServer(satoriserver.Config{})
	if err != nil {
		t.Fatalf("new server failed: %v", err)
	}
	defer srv.Close()
	srv.ReplaceRouter(parent)

	srv.RouteInternal("*", func(request *satoriserver.Request[satoriserver.InternalParam]) (any, error) {
		return map[string]any{"action": request.Action}, nil
	})

	handler, err := srv.Handler()
	if err != nil {
		t.Fatalf("build handler failed: %v", err)
	}
	httpServer := httptest.NewServer(handler)
	defer httpServer.Close()

	customResp, err := http.Get(httpServer.URL + "/custom-setter")
	if err != nil {
		t.Fatalf("custom request failed: %v", err)
	}
	defer customResp.Body.Close()
	if customResp.StatusCode != http.StatusAccepted {
		body, _ := io.ReadAll(customResp.Body)
		t.Fatalf("custom status mismatch: got %d body=%s", customResp.StatusCode, string(body))
	}

	req, err := http.NewRequest(http.MethodPost, httpServer.URL+"/v1/internal/ping", strings.NewReader(`{}`))
	if err != nil {
		t.Fatalf("new request failed: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Platform", "mock")
	req.Header.Set("X-Self-ID", "bot")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("status mismatch: got %d body=%s", resp.StatusCode, string(body))
	}
}

func TestReplaceRouterConflictPriorityAutoRegistration(t *testing.T) {
	parent := chi.NewRouter()
	parent.Post("/v1/meta", func(w http.ResponseWriter, request *http.Request) {
		w.WriteHeader(http.StatusTeapot)
		_, _ = w.Write([]byte("custom-meta"))
	})

	srv, err := satoriserver.NewServer(satoriserver.Config{ReplaceRouter: parent})
	if err != nil {
		t.Fatalf("new server failed: %v", err)
	}
	defer srv.Close()

	handler, err := srv.Handler()
	if err != nil {
		t.Fatalf("build handler failed: %v", err)
	}
	httpServer := httptest.NewServer(handler)
	defer httpServer.Close()

	req, err := http.NewRequest(http.MethodPost, httpServer.URL+"/v1/meta", strings.NewReader(`{}`))
	if err != nil {
		t.Fatalf("new request failed: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	// In chi, an exact route registered on the parent router takes precedence over
	// grouped routes under Route("/v1", ...), so user custom route remains effective.
	if resp.StatusCode != http.StatusTeapot {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("status mismatch: got %d body=%s", resp.StatusCode, string(body))
	}
}

func TestReplaceRouterConflictPriorityManualRegistrationOrder(t *testing.T) {
	parent := chi.NewRouter()
	srv, err := satoriserver.NewServer(satoriserver.Config{ReplaceRouter: parent})
	if err != nil {
		t.Fatalf("new server failed: %v", err)
	}
	defer srv.Close()

	// Register protocol routes first, then add custom route so custom wins by order.
	if err := srv.RegisterRoutes(parent); err != nil {
		t.Fatalf("register routes failed: %v", err)
	}
	parent.Post("/v1/meta", func(w http.ResponseWriter, request *http.Request) {
		w.WriteHeader(http.StatusTeapot)
		_, _ = w.Write([]byte("custom-meta"))
	})

	handler, err := srv.Handler()
	if err != nil {
		t.Fatalf("build handler failed: %v", err)
	}
	httpServer := httptest.NewServer(handler)
	defer httpServer.Close()

	req, err := http.NewRequest(http.MethodPost, httpServer.URL+"/v1/meta", strings.NewReader(`{}`))
	if err != nil {
		t.Fatalf("new request failed: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusTeapot {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("status mismatch: got %d body=%s", resp.StatusCode, string(body))
	}
}

func TestRegisterRoutesIsIdempotentForSameRouter(t *testing.T) {
	srv, err := satoriserver.NewServer(satoriserver.Config{})
	if err != nil {
		t.Fatalf("new server failed: %v", err)
	}
	defer srv.Close()

	router := chi.NewRouter()
	if err := srv.RegisterRoutes(router); err != nil {
		t.Fatalf("first register routes failed: %v", err)
	}
	if err := srv.RegisterRoutes(router); err != nil {
		t.Fatalf("second register routes failed: %v", err)
	}

	httpServer := httptest.NewServer(router)
	defer httpServer.Close()
	req, err := http.NewRequest(http.MethodPost, httpServer.URL+"/v1/meta", strings.NewReader(`{}`))
	if err != nil {
		t.Fatalf("new request failed: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("status mismatch: got %d body=%s", resp.StatusCode, string(body))
	}
}

func TestRouteHTTPAndRouteWebSocket(t *testing.T) {
	srv, err := satoriserver.NewServer(satoriserver.Config{})
	if err != nil {
		t.Fatalf("new server failed: %v", err)
	}
	defer srv.Close()

	if err := srv.RouteHTTP("/ext-http", nil, http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})); err != nil {
		t.Fatalf("route http failed: %v", err)
	}
	if err := srv.RouteWebSocket("/ext-ws", http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		w.WriteHeader(http.StatusAccepted)
	})); err != nil {
		t.Fatalf("route websocket failed: %v", err)
	}

	handler, err := srv.Handler()
	if err != nil {
		t.Fatalf("build handler failed: %v", err)
	}
	httpServer := httptest.NewServer(handler)
	defer httpServer.Close()

	httpResp, err := http.Get(httpServer.URL + "/ext-http")
	if err != nil {
		t.Fatalf("http extension request failed: %v", err)
	}
	defer httpResp.Body.Close()
	if httpResp.StatusCode != http.StatusNoContent {
		body, _ := io.ReadAll(httpResp.Body)
		t.Fatalf("http extension status mismatch: got %d body=%s", httpResp.StatusCode, string(body))
	}

	wsResp, err := http.Get(httpServer.URL + "/ext-ws")
	if err != nil {
		t.Fatalf("ws extension request failed: %v", err)
	}
	defer wsResp.Body.Close()
	if wsResp.StatusCode != http.StatusAccepted {
		body, _ := io.ReadAll(wsResp.Body)
		t.Fatalf("ws extension status mismatch: got %d body=%s", wsResp.StatusCode, string(body))
	}
}

func TestWebhookCreateOnlyDependsOnProxyURLs(t *testing.T) {
	bodyCh := make(chan []byte, 1)
	webhook := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		defer request.Body.Close()
		data, err := io.ReadAll(request.Body)
		if err != nil {
			bodyCh <- nil
		} else {
			bodyCh <- append([]byte(nil), data...)
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer webhook.Close()

	srv, err := satoriserver.NewServer(satoriserver.Config{})
	if err != nil {
		t.Fatalf("new server failed: %v", err)
	}
	defer srv.Close()

	if applyErr := srv.Apply(&failingLoginProvider{
		proxyURLs: []string{"https://proxy.example/a", "https://proxy.example/b"},
		loginErr:  errors.New("login backend unavailable"),
	}); applyErr != nil {
		t.Fatalf("apply provider failed: %v", applyErr)
	}

	handler, err := srv.Handler()
	if err != nil {
		t.Fatalf("build handler failed: %v", err)
	}
	httpServer := httptest.NewServer(handler)
	defer httpServer.Close()

	reqBody := `{"url":"` + webhook.URL + `"}`
	req, err := http.NewRequest(http.MethodPost, httpServer.URL+"/v1/meta/webhook.create", strings.NewReader(reqBody))
	if err != nil {
		t.Fatalf("new request failed: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("status mismatch: got %d body=%s", resp.StatusCode, string(body))
	}

	data := <-bodyCh
	if data == nil {
		t.Fatal("webhook body read failed")
	}
	var payload struct {
		ProxyURLs []string `json:"proxy_urls"`
	}
	if err := json.Unmarshal(data, &payload); err != nil {
		t.Fatalf("decode webhook body failed: %v", err)
	}
	if len(payload.ProxyURLs) != 2 || payload.ProxyURLs[0] != "https://proxy.example/a" || payload.ProxyURLs[1] != "https://proxy.example/b" {
		t.Fatalf("proxy_urls mismatch: %#v", payload.ProxyURLs)
	}
}

func TestMetaGetStillDependsOnLogins(t *testing.T) {
	srv, err := satoriserver.NewServer(satoriserver.Config{})
	if err != nil {
		t.Fatalf("new server failed: %v", err)
	}
	defer srv.Close()

	if applyErr := srv.Apply(&failingLoginProvider{
		proxyURLs: []string{"https://proxy.example/a"},
		loginErr:  errors.New("login backend unavailable"),
	}); applyErr != nil {
		t.Fatalf("apply provider failed: %v", applyErr)
	}

	handler, err := srv.Handler()
	if err != nil {
		t.Fatalf("build handler failed: %v", err)
	}
	httpServer := httptest.NewServer(handler)
	defer httpServer.Close()

	req, err := http.NewRequest(http.MethodPost, httpServer.URL+"/v1/meta", strings.NewReader(`{}`))
	if err != nil {
		t.Fatalf("new request failed: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusInternalServerError {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("status mismatch: got %d body=%s", resp.StatusCode, string(body))
	}
}

func TestRegisterRoutesUnderSubPath(t *testing.T) {
	srv, err := satoriserver.NewServer(satoriserver.Config{})
	if err != nil {
		t.Fatalf("new server failed: %v", err)
	}
	defer srv.Close()

	srv.RouteInternal("*", func(request *satoriserver.Request[satoriserver.InternalParam]) (any, error) {
		return map[string]any{"action": request.Action}, nil
	})

	parent := chi.NewRouter()
	parent.Route("/satori", func(r chi.Router) {
		if registerErr := srv.RegisterRoutes(r); registerErr != nil {
			t.Fatalf("register routes failed: %v", registerErr)
		}
	})

	httpServer := httptest.NewServer(parent)
	defer httpServer.Close()

	req, err := http.NewRequest(http.MethodPost, httpServer.URL+"/satori/v1/internal/ping", strings.NewReader(`{}`))
	if err != nil {
		t.Fatalf("new request failed: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Platform", "mock")
	req.Header.Set("X-Self-ID", "bot")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("status mismatch: got %d body=%s", resp.StatusCode, string(body))
	}

	var payload map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		t.Fatalf("decode response failed: %v", err)
	}
	if payload["action"] != "internal/ping" {
		t.Fatalf("unexpected action: %#v", payload["action"])
	}
}

func TestRegisterRoutesOnParentRouter(t *testing.T) {
	srv, err := satoriserver.NewServer(satoriserver.Config{})
	if err != nil {
		t.Fatalf("new server failed: %v", err)
	}
	defer srv.Close()

	srv.RouteInternal("*", func(request *satoriserver.Request[satoriserver.InternalParam]) (any, error) {
		return map[string]any{"ok": true, "action": request.Action}, nil
	})

	parent := chi.NewRouter()
	if err := srv.RegisterRoutes(parent); err != nil {
		t.Fatalf("register routes failed: %v", err)
	}

	httpServer := httptest.NewServer(parent)
	defer httpServer.Close()

	req, err := http.NewRequest(http.MethodPost, httpServer.URL+"/v1/internal/register-check", strings.NewReader(`{}`))
	if err != nil {
		t.Fatalf("new request failed: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Platform", "mock")
	req.Header.Set("X-Self-ID", "bot")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("status mismatch: got %d body=%s", resp.StatusCode, string(body))
	}
}

func TestRootRouteRegistration(t *testing.T) {
	srv, err := satoriserver.NewServer(satoriserver.Config{})
	if err != nil {
		t.Fatalf("new server failed: %v", err)
	}
	defer srv.Close()

	err = srv.Handle("/healthz", http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	if err != nil {
		t.Fatalf("register root route failed: %v", err)
	}

	handler, err := srv.Handler()
	if err != nil {
		t.Fatalf("build handler failed: %v", err)
	}
	httpServer := httptest.NewServer(handler)
	defer httpServer.Close()

	resp, err := http.Get(httpServer.URL + "/healthz")
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("status mismatch: got %d body=%s", resp.StatusCode, string(body))
	}
}

func TestMountFileRejectsDirectoryPath(t *testing.T) {
	tmpDir := t.TempDir()

	srv, err := satoriserver.NewServer(satoriserver.Config{})
	if err != nil {
		t.Fatalf("new server failed: %v", err)
	}
	defer srv.Close()

	if err := srv.MountFile("/static", tmpDir); err != nil {
		t.Fatalf("mount file should defer validation to build phase, got: %v", err)
	}

	_, err = srv.Handler()
	if err == nil {
		t.Fatal("expected handler build error for directory path in MountFile")
	}
	if !strings.Contains(err.Error(), "use MountDir") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestMountDirServesDirectory(t *testing.T) {
	tmpDir := t.TempDir()
	target := filepath.Join(tmpDir, "sample.txt")
	if writeErr := os.WriteFile(target, []byte("dir-mounted"), 0o600); writeErr != nil {
		t.Fatalf("write sample file failed: %v", writeErr)
	}

	srv, err := satoriserver.NewServer(satoriserver.Config{})
	if err != nil {
		t.Fatalf("new server failed: %v", err)
	}
	defer srv.Close()

	if err := srv.MountDir("/assets", tmpDir, false); err != nil {
		t.Fatalf("mount dir failed: %v", err)
	}

	handler, err := srv.Handler()
	if err != nil {
		t.Fatalf("build handler failed: %v", err)
	}
	httpServer := httptest.NewServer(handler)
	defer httpServer.Close()

	resp, err := http.Get(httpServer.URL + "/assets/sample.txt")
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("status mismatch: got %d body=%s", resp.StatusCode, string(body))
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read body failed: %v", err)
	}
	if string(body) != "dir-mounted" {
		t.Fatalf("body mismatch: %q", string(body))
	}
}

func TestJSONPayloadCompatibility(t *testing.T) {
	srv, err := satoriserver.NewServer(satoriserver.Config{})
	if err != nil {
		t.Fatalf("new server failed: %v", err)
	}
	defer srv.Close()

	srv.RouteInternal("*", func(request *satoriserver.Request[satoriserver.InternalParam]) (any, error) {
		return request.Params, nil
	})

	handler, err := srv.Handler()
	if err != nil {
		t.Fatalf("build handler failed: %v", err)
	}
	httpServer := httptest.NewServer(handler)
	defer httpServer.Close()

	payload := `{"text":"\u4f60\u597d","number":12345678901234567890,"nested":{"k":"v"},"list":[1,2,3]}`
	req, err := http.NewRequest(http.MethodPost, httpServer.URL+"/v1/internal/json-echo", strings.NewReader(payload))
	if err != nil {
		t.Fatalf("new request failed: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Platform", "mock")
	req.Header.Set("X-Self-ID", "bot")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("status mismatch: got %d body=%s", resp.StatusCode, string(body))
	}

	decoder := json.NewDecoder(resp.Body)
	decoder.UseNumber()
	parsed := map[string]any{}
	if err := decoder.Decode(&parsed); err != nil {
		t.Fatalf("decode response failed: %v", err)
	}
	if parsed["text"] != "\u4f60\u597d" {
		t.Fatalf("unicode field mismatch: %#v", parsed["text"])
	}
	number, ok := parsed["number"].(json.Number)
	if !ok {
		t.Fatalf("number type mismatch: %T", parsed["number"])
	}
	if number.String() != "12345678901234567890" {
		t.Fatalf("number value mismatch: %s", number.String())
	}
}

func TestJSONInvalidReturnsBadRequest(t *testing.T) {
	srv, err := satoriserver.NewServer(satoriserver.Config{})
	if err != nil {
		t.Fatalf("new server failed: %v", err)
	}
	defer srv.Close()

	srv.RouteInternal("*", func(request *satoriserver.Request[satoriserver.InternalParam]) (any, error) {
		return request.Params, nil
	})

	handler, err := srv.Handler()
	if err != nil {
		t.Fatalf("build handler failed: %v", err)
	}
	httpServer := httptest.NewServer(handler)
	defer httpServer.Close()

	req, err := http.NewRequest(http.MethodPost, httpServer.URL+"/v1/internal/json-invalid", bytes.NewBufferString(`{"missing":`))
	if err != nil {
		t.Fatalf("new request failed: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Platform", "mock")
	req.Header.Set("X-Self-ID", "bot")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("status mismatch: got %d body=%s", resp.StatusCode, string(body))
	}
}

func TestJSONEmptyBodyDefaultsToEmptyObject(t *testing.T) {
	srv, err := satoriserver.NewServer(satoriserver.Config{})
	if err != nil {
		t.Fatalf("new server failed: %v", err)
	}
	defer srv.Close()

	srv.RouteInternal("*", func(request *satoriserver.Request[satoriserver.InternalParam]) (any, error) {
		return request.Params, nil
	})

	handler, err := srv.Handler()
	if err != nil {
		t.Fatalf("build handler failed: %v", err)
	}
	httpServer := httptest.NewServer(handler)
	defer httpServer.Close()

	req, err := http.NewRequest(http.MethodPost, httpServer.URL+"/v1/internal/empty", http.NoBody)
	if err != nil {
		t.Fatalf("new request failed: %v", err)
	}
	req.Header.Set("X-Platform", "mock")
	req.Header.Set("X-Self-ID", "bot")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("status mismatch: got %d body=%s", resp.StatusCode, string(body))
	}

	parsed := map[string]any{}
	if err := json.NewDecoder(resp.Body).Decode(&parsed); err != nil {
		t.Fatalf("decode response failed: %v", err)
	}
	if len(parsed) != 0 {
		t.Fatalf("expected empty object, got %#v", parsed)
	}
}
