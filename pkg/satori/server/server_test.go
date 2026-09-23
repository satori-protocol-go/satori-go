package server_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"github.com/go-chi/chi/v5"
	"github.com/gorilla/websocket"
	"github.com/satori-protocol-go/satori-go/pkg/satori/model/event"
	"github.com/satori-protocol-go/satori-go/pkg/satori/model/login"
	"github.com/satori-protocol-go/satori-go/pkg/satori/model/operation"
	"github.com/satori-protocol-go/satori-go/pkg/satori/model/user"
	"github.com/satori-protocol-go/satori-go/pkg/satori/protocol"
	satoriserver "github.com/satori-protocol-go/satori-go/pkg/satori/server"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

type mockProvider struct {
	logins    []*login.Login
	proxyUrls []string
}

func (m *mockProvider) GetLogins(context.Context) ([]*login.Login, error) {
	return m.logins, nil
}

func (m *mockProvider) ProxyUrls() []string {
	copied := make([]string, len(m.proxyUrls))
	copy(copied, m.proxyUrls)
	return copied
}

func (m *mockProvider) Ensure(platform string, selfID string) bool {
	return platform == "mock" && selfID == "bot"
}

func (m *mockProvider) HandleInternal(
	request satoriserver.Request[map[string]any],
	path string,
) (*satoriserver.Response, error) {
	_ = request
	_ = path
	return nil, satoriserver.NotFound("not found")
}

func (m *mockProvider) HandleProxied(prefix string, rawURL string) (*satoriserver.Response, error) {
	_ = prefix
	_ = rawURL
	return nil, nil
}

func TestServerHTTPRouteDispatch(t *testing.T) {
	server, err := satoriserver.NewServer(satoriserver.Config{})
	if err != nil {
		t.Fatalf("new server failed: %v", err)
	}
	defer server.Close()

	called := false
	server.Route(protocol.ApiMessageCreate, func(request *satoriserver.Request[any]) (any, error) {
		called = true
		if request.Action != string(protocol.ApiMessageCreate) {
			t.Fatalf("unexpected action: %s", request.Action)
		}
		return map[string]any{
			"id":      "123",
			"content": "ok",
		}, nil
	})

	httpServer := newTestHTTPServer(t, server)
	defer httpServer.Close()

	{
		resp, err := http.Post(httpServer.URL+"/v1/message.create", "application/json", strings.NewReader(`{}`))
		if err != nil {
			t.Fatalf("http post failed: %v", err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusUnauthorized {
			t.Fatalf("missing header status mismatch: got %d want %d", resp.StatusCode, http.StatusUnauthorized)
		}
	}

	req, err := http.NewRequest(http.MethodPost, httpServer.URL+"/v1/message.create", strings.NewReader(`{"content":"hello"}`))
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
		t.Fatalf("status mismatch: got %d want %d", resp.StatusCode, http.StatusOK)
	}
	if !called {
		t.Fatal("route handler not called")
	}

	var body map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode response failed: %v", err)
	}
	if body["id"] != "123" {
		t.Fatalf("id mismatch: %#v", body["id"])
	}
}

func TestServerInternalWildcardRoute(t *testing.T) {
	server, err := satoriserver.NewServer(satoriserver.Config{})
	if err != nil {
		t.Fatalf("new server failed: %v", err)
	}
	defer server.Close()

	server.Route(protocol.ParseApi("internal/*"), func(request *satoriserver.Request[any]) (any, error) {
		return map[string]any{"action": request.Action}, nil
	})

	httpServer := newTestHTTPServer(t, server)
	defer httpServer.Close()

	req, err := http.NewRequest(http.MethodGet, httpServer.URL+"/v1/internal/ping", nil)
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
		t.Fatalf("status mismatch: got %d want %d", resp.StatusCode, http.StatusOK)
	}

	var body map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode response failed: %v", err)
	}
	if body["action"] != "internal/ping" {
		t.Fatalf("unexpected action: %#v", body["action"])
	}
}

func TestServerWebSocketAndPost(t *testing.T) {
	server, err := satoriserver.NewServer(satoriserver.Config{
		Token: "secret",
	})
	if err != nil {
		t.Fatalf("new server failed: %v", err)
	}
	defer server.Close()

	err = server.Apply(&mockProvider{
		logins: []*login.Login{
			{
				Sn:       1,
				Platform: "mock",
				User:     &user.User{Id: "bot"},
				Status:   login.LoginStatusOnline,
				Adapter:  "mock",
			},
		},
		proxyUrls: []string{"https://example.com"},
	})
	if err != nil {
		t.Fatalf("apply provider failed: %v", err)
	}

	httpServer := newTestHTTPServer(t, server)
	defer httpServer.Close()

	wsURL := "ws" + strings.TrimPrefix(httpServer.URL, "http") + "/v1/events"
	connection, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("dial websocket failed: %v", err)
	}
	defer connection.Close()

	if err := connection.WriteJSON(map[string]any{
		"op": operation.OpcodeIdentify,
		"body": map[string]any{
			"token":    "secret",
			"sequence": -1,
		},
	}); err != nil {
		t.Fatalf("send identify failed: %v", err)
	}

	_ = connection.SetReadDeadline(time.Now().Add(3 * time.Second))
	var ready map[string]any
	if err := connection.ReadJSON(&ready); err != nil {
		t.Fatalf("read ready failed: %v", err)
	}
	if toInt(t, ready["op"]) != int(operation.OpcodeReady) {
		t.Fatalf("ready opcode mismatch: %#v", ready["op"])
	}

	err = server.Post(&event.Event{
		Type:      event.EventTypeMessageCreated,
		Timestamp: time.Now().UnixMilli(),
		Login: &login.Login{
			Sn:       1,
			Platform: "mock",
			User:     &user.User{Id: "bot"},
			Status:   login.LoginStatusOnline,
			Adapter:  "mock",
		},
	})
	if err != nil {
		t.Fatalf("post event failed: %v", err)
	}

	_ = connection.SetReadDeadline(time.Now().Add(3 * time.Second))
	var pushed map[string]any
	if err := connection.ReadJSON(&pushed); err != nil {
		t.Fatalf("read event failed: %v", err)
	}
	if toInt(t, pushed["op"]) != int(operation.OpcodeEvent) {
		t.Fatalf("event opcode mismatch: %#v", pushed["op"])
	}

	if err := connection.WriteJSON(map[string]any{"op": operation.OpcodePing}); err != nil {
		t.Fatalf("send ping failed: %v", err)
	}

	_ = connection.SetReadDeadline(time.Now().Add(3 * time.Second))
	var pong map[string]any
	if err := connection.ReadJSON(&pong); err != nil {
		t.Fatalf("read pong failed: %v", err)
	}
	if toInt(t, pong["op"]) != int(operation.OpcodePong) {
		t.Fatalf("pong opcode mismatch: %#v", pong["op"])
	}
}

func TestServerDefaultUploadAndProxy(t *testing.T) {
	server, err := satoriserver.NewServer(satoriserver.Config{})
	if err != nil {
		t.Fatalf("new server failed: %v", err)
	}
	defer server.Close()

	httpServer := newTestHTTPServer(t, server)
	defer httpServer.Close()

	buffer := &bytes.Buffer{}
	writer := multipart.NewWriter(buffer)
	fileWriter, err := writer.CreateFormFile("file", "demo.txt")
	if err != nil {
		t.Fatalf("create form file failed: %v", err)
	}
	if _, err := fileWriter.Write([]byte("hello-upload")); err != nil {
		t.Fatalf("write form file failed: %v", err)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("close multipart writer failed: %v", err)
	}

	uploadReq, err := http.NewRequest(http.MethodPost, httpServer.URL+"/v1/upload.create", buffer)
	if err != nil {
		t.Fatalf("new upload request failed: %v", err)
	}
	uploadReq.Header.Set("Content-Type", writer.FormDataContentType())
	uploadReq.Header.Set("X-Platform", "mock")
	uploadReq.Header.Set("X-Self-ID", "bot")

	uploadResp, err := http.DefaultClient.Do(uploadReq)
	if err != nil {
		t.Fatalf("upload request failed: %v", err)
	}
	defer uploadResp.Body.Close()
	if uploadResp.StatusCode != http.StatusOK {
		data, _ := io.ReadAll(uploadResp.Body)
		t.Fatalf("upload status mismatch: %d body=%s", uploadResp.StatusCode, string(data))
	}

	var payload map[string]string
	if err := json.NewDecoder(uploadResp.Body).Decode(&payload); err != nil {
		t.Fatalf("decode upload response failed: %v", err)
	}

	internalURL, ok := payload["file"]
	if !ok || internalURL == "" {
		t.Fatalf("missing upload result: %#v", payload)
	}

	proxyURL := httpServer.URL + "/v1/proxy/" + url.PathEscape(internalURL)
	proxyResp, err := http.Get(proxyURL)
	if err != nil {
		t.Fatalf("proxy request failed: %v", err)
	}
	defer proxyResp.Body.Close()
	if proxyResp.StatusCode != http.StatusOK {
		data, _ := io.ReadAll(proxyResp.Body)
		t.Fatalf("proxy status mismatch: %d body=%s", proxyResp.StatusCode, string(data))
	}

	data, err := io.ReadAll(proxyResp.Body)
	if err != nil {
		t.Fatalf("read proxy body failed: %v", err)
	}
	if string(data) != "hello-upload" {
		t.Fatalf("proxy body mismatch: %q", string(data))
	}
}

func TestDecodeMessageListParamStrictNumber(t *testing.T) {
	_, err := decodeTestParams[satoriserver.MessageListParam](map[string]any{
		"channel_id": "c1",
		"limit":      "10",
	})
	if err == nil {
		t.Fatal("expected error for string limit")
	}

	_, err = decodeTestParams[satoriserver.MessageListParam](map[string]any{
		"channel_id": "c1",
		"limit":      1.5,
	})
	if err == nil {
		t.Fatal("expected error for fractional limit")
	}
}

func TestDecodeMessageListParamSafeInteger(t *testing.T) {
	_, err := decodeTestParams[satoriserver.MessageListParam](map[string]any{
		"channel_id": "c1",
		"limit":      int64(9007199254740992),
	})
	if err == nil {
		t.Fatal("expected error for out-of-range limit")
	}

	params, err := decodeTestParams[satoriserver.MessageListParam](map[string]any{
		"channel_id": "c1",
		"limit":      int64(100),
	})
	if err != nil {
		t.Fatalf("decode valid limit failed: %v", err)
	}
	limit, ok := params.Limit.Get()
	if !ok || limit != 100 {
		t.Fatalf("limit parse mismatch: ok=%v value=%d", ok, limit)
	}
}

func TestDecodeMessageListParamOptionMissing(t *testing.T) {
	params, err := decodeTestParams[satoriserver.MessageListParam](map[string]any{
		"channel_id": "c1",
	})
	if err != nil {
		t.Fatalf("decode failed: %v", err)
	}
	if params.Limit.IsSome() {
		t.Fatal("limit should be none when field is missing")
	}
}

func TestDecodeGuildMemberMuteParamStrictNumber(t *testing.T) {
	_, err := decodeTestParams[satoriserver.GuildMemberMuteParam](map[string]any{
		"guild_id": "g1",
		"user_id":  "u1",
		"duration": "1000",
	})
	if err == nil {
		t.Fatal("expected error for string duration")
	}

	_, err = decodeTestParams[satoriserver.GuildMemberMuteParam](map[string]any{
		"guild_id": "g1",
		"user_id":  "u1",
		"duration": int64(9007199254740992),
	})
	if err == nil {
		t.Fatal("expected error for out-of-range duration")
	}
}

func toInt(t *testing.T, value any) int {
	t.Helper()
	switch typed := value.(type) {
	case float64:
		return int(typed)
	case int:
		return typed
	case json.Number:
		i, err := typed.Int64()
		if err != nil {
			t.Fatalf("json number to int failed: %v", err)
		}
		return int(i)
	default:
		t.Fatalf("unsupported number type: %T", value)
		return 0
	}
}

type failingLoginProvider struct {
	proxyURLs []string
	loginErr  error
}

func (p *failingLoginProvider) GetLogins(context.Context) ([]*login.Login, error) {
	if p.loginErr != nil {
		return nil, p.loginErr
	}
	return []*login.Login{}, nil
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

	srv.Route(protocol.ApiMessageCreate, func(request *satoriserver.Request[any]) (any, error) {
		return request.Params, nil
	})

	handler, err := srv.Handler()
	if err != nil {
		t.Fatalf("build handler failed: %v", err)
	}
	httpServer := httptest.NewServer(handler)
	defer httpServer.Close()

	payload := `{"text":"\u4f60\u597d","number":12345678901234567890,"nested":{"k":"v"},"list":[1,2,3]}`
	req, err := http.NewRequest(http.MethodPost, httpServer.URL+"/v1/message.create", strings.NewReader(payload))
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

	srv.Route(protocol.ApiMessageCreate, func(request *satoriserver.Request[any]) (any, error) {
		return request.Params, nil
	})

	handler, err := srv.Handler()
	if err != nil {
		t.Fatalf("build handler failed: %v", err)
	}
	httpServer := httptest.NewServer(handler)
	defer httpServer.Close()

	req, err := http.NewRequest(http.MethodPost, httpServer.URL+"/v1/message.create", bytes.NewBufferString(`{"missing":`))
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

	srv.Route(protocol.ApiMessageCreate, func(request *satoriserver.Request[any]) (any, error) {
		return request.Params, nil
	})

	handler, err := srv.Handler()
	if err != nil {
		t.Fatalf("build handler failed: %v", err)
	}
	httpServer := httptest.NewServer(handler)
	defer httpServer.Close()

	req, err := http.NewRequest(http.MethodPost, httpServer.URL+"/v1/message.create", http.NoBody)
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

func newTestHTTPServer(t *testing.T, srv *satoriserver.Server) *httptest.Server {
	t.Helper()
	handler, err := srv.Handler()
	if err != nil {
		t.Fatal(err)
	}
	return httptest.NewServer(handler)
}

func decodeTestParams[T any](value any) (T, error) {
	var result T
	handler := satoriserver.Wrapper(func(r *satoriserver.Request[T]) (any, error) { result = r.Params; return nil, nil })
	_, err := handler(&satoriserver.Request[any]{Params: value})
	return result, err
}

func TestProtocolAuthorization(t *testing.T) {
	srv, err := satoriserver.NewServer(satoriserver.Config{Token: "fixture-secret"})
	if err != nil {
		t.Fatal(err)
	}
	defer srv.Close()
	srv.Route(protocol.ApiMessageCreate, func(r *satoriserver.Request[any]) (any, error) {
		return []map[string]string{{"id": "sent", "content": "hello"}}, nil
	})
	if err := srv.Method(http.MethodPost, "/platform-callback", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(202) })); err != nil {
		t.Fatal(err)
	}
	handler, err := srv.Handler()
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"/v1/message.create", "/v1/meta", "/v1/meta/webhook.delete"} {
		for _, auth := range []string{"", "Bearer wrong", "Bearer fixture-secret"} {
			r := httptest.NewRequest(http.MethodPost, path, strings.NewReader(`{"url":"http://fixture.invalid"}`))
			r.Header.Set("Satori-Platform", "mock")
			r.Header.Set("Satori-User-ID", "bot")
			r.Header.Set("Authorization", auth)
			w := httptest.NewRecorder()
			handler.ServeHTTP(w, r)
			want := 401
			if auth == "Bearer fixture-secret" {
				want = 200
			}
			if w.Code != want {
				t.Errorf("%s auth=%q status=%d body=%s", path, auth, w.Code, w.Body)
			}
		}
	}
	r := httptest.NewRequest(http.MethodPost, "/platform-callback", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)
	if w.Code != 202 {
		t.Fatalf("platform callback=%d", w.Code)
	}
	r = httptest.NewRequest(http.MethodGet, "/v1/proxy/"+url.PathEscape("internal:mock/bot/_api/users"), nil)
	w = httptest.NewRecorder()
	handler.ServeHTTP(w, r)
	if w.Code != 401 {
		t.Fatalf("native proxy authorization=%d", w.Code)
	}
}
