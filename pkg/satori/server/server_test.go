package server_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/go-chi/chi/v5"
	"github.com/gorilla/websocket"
	"github.com/satori-protocol-go/satori-go/pkg/satori/model/event"
	"github.com/satori-protocol-go/satori-go/pkg/satori/model/login"
	"github.com/satori-protocol-go/satori-go/pkg/satori/model/message"
	"github.com/satori-protocol-go/satori-go/pkg/satori/model/operation"
	"github.com/satori-protocol-go/satori-go/pkg/satori/model/user"
	"github.com/satori-protocol-go/satori-go/pkg/satori/protocol"
	satoriserver "github.com/satori-protocol-go/satori-go/pkg/satori/server"
	"io"
	"mime/multipart"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
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
	for _, info := range m.logins {
		if info.User != nil && info.Platform == platform && info.User.Id == selfID {
			return true
		}
	}
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

func (m *mockProvider) HandleProxied(ctx context.Context, prefix string, rawURL string) (*satoriserver.Response, error) {
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
	if err := server.Apply(&mockProvider{logins: []*login.Login{{Sn: 1, Platform: "mock", User: &user.User{Id: "other"}}}}); err != nil {
		t.Fatal(err)
	}

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
	local, err := server.GetLocalFile(internalURL)
	if err != nil || string(local) != "hello-upload" {
		t.Fatalf("owned resource=%q error=%v", local, err)
	}
	for _, tc := range []struct {
		url    string
		status int
	}{
		{strings.Replace(internalURL, "/bot/", "/other/", 1), 403},
		{"internal:mock/bot/_tmp/../outside", 400},
		{"internal:mock/missing/_tmp/file", 404},
	} {
		response, err := http.Get(httpServer.URL + "/v1/proxy/" + url.PathEscape(tc.url))
		if err != nil {
			t.Fatal(err)
		}
		response.Body.Close()
		if response.StatusCode != tc.status {
			t.Errorf("resource %s status=%d", tc.url, response.StatusCode)
		}
	}
	small, err := satoriserver.NewServer(satoriserver.Config{MaxRequestBytes: 64})
	if err != nil {
		t.Fatal(err)
	}
	defer small.Close()
	if err := small.Apply(&mockProvider{}); err != nil {
		t.Fatal(err)
	}
	handler, err := small.Handler()
	if err != nil {
		t.Fatal(err)
	}
	body := bytes.NewBuffer(nil)
	form := multipart.NewWriter(body)
	out, err := form.CreateFormFile("file", "large.bin")
	if err != nil {
		t.Fatal(err)
	}
	out.Write(bytes.Repeat([]byte("x"), 128))
	form.Close()
	req := httptest.NewRequest("POST", "/v1/upload.create", body)
	req.Header.Set("Content-Type", form.FormDataContentType())
	req.Header.Set("Satori-Platform", "mock")
	req.Header.Set("Satori-User-ID", "bot")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != 413 {
		t.Fatalf("limited upload=%d %s", rec.Code, rec.Body)
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

func (p *failingLoginProvider) HandleProxied(ctx context.Context, _ string, _ string) (*satoriserver.Response, error) {
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

type preparingFailure struct {
	satoriserver.RouterMixin
	mockProvider
	started chan struct{}
	failure error
}

func (p *preparingFailure) EnsureServer(*satoriserver.Server) {}
func (p *preparingFailure) Prepare(ctx context.Context) error {
	close(p.started)
	<-ctx.Done()
	return p.failure
}

func TestServerShutdownResult(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := listener.Addr().(*net.TCPAddr).Port
	listener.Close()
	srv, err := satoriserver.NewServer(satoriserver.Config{Port: port})
	if err != nil {
		t.Fatal(err)
	}
	defer srv.Close()
	failure := errors.New("fixture prepare failure")
	adapter := &preparingFailure{started: make(chan struct{}), failure: failure}
	if err := srv.Apply(adapter); err != nil {
		t.Fatal(err)
	}
	result := make(chan error, 1)
	go func() { result <- srv.Run(context.Background()) }()
	select {
	case <-adapter.started:
	case <-time.After(time.Second):
		t.Fatal("prepare timeout")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	shutdownErr := srv.Shutdown(ctx)
	if !errors.Is(shutdownErr, failure) {
		t.Fatalf("Shutdown result=%v", shutdownErr)
	}
	select {
	case runErr := <-result:
		if !errors.Is(runErr, failure) {
			t.Fatalf("Run result=%v", runErr)
		}
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
}

type eventProvider struct {
	mockProvider
	events chan *event.Event
}

func (p *eventProvider) Publisher(context.Context) <-chan *event.Event { return p.events }

func TestProviderEventsAndSnapshots(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := listener.Addr().(*net.TCPAddr).Port
	listener.Close()
	srv, err := satoriserver.NewServer(satoriserver.Config{Port: port, Token: "fixture"})
	if err != nil {
		t.Fatal(err)
	}
	defer srv.Close()
	alpha := &login.Login{Sn: 0, Platform: "mock", User: &user.User{Id: "alpha"}, Status: login.LoginStatusOnline, Adapter: "fixture"}
	beta := &login.Login{Sn: 0, Platform: "mock", User: &user.User{Id: "beta"}, Status: login.LoginStatusOnline, Adapter: "fixture"}
	one := &eventProvider{mockProvider: mockProvider{logins: []*login.Login{alpha}}, events: make(chan *event.Event, 2)}
	two := &eventProvider{mockProvider: mockProvider{logins: []*login.Login{beta}}, events: make(chan *event.Event, 2)}
	if err := srv.Apply(one); err != nil {
		t.Fatal(err)
	}
	if err := srv.Apply(two); err != nil {
		t.Fatal(err)
	}
	srv.Route(protocol.ApiLoginGet, func(r *satoriserver.Request[any]) (any, error) {
		if r.SelfID == "alpha" {
			return alpha.Clone(), nil
		}
		return beta.Clone(), nil
	})
	ctx, cancel := context.WithCancel(context.Background())
	finished := make(chan error, 1)
	go func() { finished <- srv.Run(ctx) }()
	defer func() {
		cancel()
		select {
		case err := <-finished:
			if err != nil {
				t.Error(err)
			}
		case <-time.After(3 * time.Second):
			t.Error("server completion timeout")
		}
	}()
	endpoint := fmt.Sprintf("ws://127.0.0.1:%d/v1/events", port)
	connect := func(sn *int64) (*websocket.Conn, []*login.Login) {
		t.Helper()
		var conn *websocket.Conn
		deadline := time.Now().Add(2 * time.Second)
		for time.Now().Before(deadline) {
			conn, _, err = websocket.DefaultDialer.Dial(endpoint, nil)
			if err == nil {
				break
			}
			time.Sleep(10 * time.Millisecond)
		}
		if err != nil {
			t.Fatal(err)
		}
		conn.SetReadDeadline(time.Now().Add(2 * time.Second))
		if err := conn.WriteJSON(operation.Operation{Op: operation.OpcodeIdentify, Body: operation.IdentifyBody{Token: "fixture", Sn: sn}}); err != nil {
			t.Fatal(err)
		}
		var ready struct {
			Op   operation.Opcode    `json:"op"`
			Body operation.ReadyBody `json:"body"`
		}
		if err := conn.ReadJSON(&ready); err != nil {
			t.Fatal(err)
		}
		if ready.Op != operation.OpcodeReady {
			t.Fatalf("ready opcode=%d", ready.Op)
		}
		return conn, ready.Body.Logins
	}
	conn, logins := connect(nil)
	defer conn.Close()
	if len(logins) != 2 || logins[0].Sn == logins[1].Sn {
		t.Fatalf("downstream logins=%+v", logins)
	}
	numbers := map[string]int64{}
	for _, info := range logins {
		numbers[info.User.Id] = info.Sn
	}
	for id, sn := range numbers {
		req, err := http.NewRequest("POST", fmt.Sprintf("http://127.0.0.1:%d/v1/login.get", port), strings.NewReader("{}"))
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set("Authorization", "Bearer fixture")
		protocol.SetIdentityHeaders(req.Header, "mock", id)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		var info login.Login
		err = json.NewDecoder(resp.Body).Decode(&info)
		resp.Body.Close()
		if err != nil || resp.StatusCode != 200 || info.Sn != sn {
			t.Errorf("login.get %s sn=%d, READY sn=%d status=%d err=%v", id, info.Sn, sn, resp.StatusCode, err)
		}
	}
	readEvent := func(connection *websocket.Conn) *event.Event {
		t.Helper()
		var frame struct {
			Op   operation.Opcode `json:"op"`
			Body event.Event      `json:"body"`
		}
		if err := connection.ReadJSON(&frame); err != nil {
			t.Fatal(err)
		}
		if frame.Op != operation.OpcodeEvent {
			t.Fatalf("event opcode=%d", frame.Op)
		}
		return &frame.Body
	}
	first := &event.Event{Sn: 91, Type: event.EventTypeMessageCreated, Timestamp: 1, Login: alpha, Message: &message.Message{Id: "a", Content: "alpha"}}
	one.events <- first
	received := readEvent(conn)
	if received.Sn != 0 || received.Login.Sn != numbers["alpha"] || received.Login.User.Id != "alpha" {
		t.Fatalf("first event=%+v", received)
	}
	second := &event.Event{Sn: 92, Type: event.EventTypeMessageCreated, Timestamp: 2, Login: beta, Message: &message.Message{Id: "b", Content: "original"}, Data_: map[string]any{"value": "original"}}
	two.events <- second
	received = readEvent(conn)
	if received.Sn != 1 || received.Login.Sn != numbers["beta"] || received.Login.User.Id != "beta" {
		t.Fatalf("second event=%+v", received)
	}
	if first.Sn != 91 || second.Sn != 92 || beta.Sn != 0 {
		t.Fatalf("source numbers=%d,%d,%d", first.Sn, second.Sn, beta.Sn)
	}
	second.Message.Content = "changed"
	second.Data_.(map[string]any)["value"] = "changed"
	resume := int64(0)
	replay, again := connect(&resume)
	defer replay.Close()
	if len(again) != 2 || again[0].Sn != logins[0].Sn || again[1].Sn != logins[1].Sn {
		t.Fatalf("ready mapping changed=%+v", again)
	}
	received = readEvent(replay)
	if received.Sn != 1 || received.Message.Content != "original" || received.Data_.(map[string]any)["value"] != "original" || received.Login.Sn != numbers["beta"] {
		t.Fatalf("frozen replay=%+v", received)
	}
}

func TestReplayAndConcurrentPublication(t *testing.T) {
	srv, err := satoriserver.NewServer(satoriserver.Config{Token: "fixture"})
	if err != nil {
		t.Fatal(err)
	}
	defer srv.Close()
	source := &login.Login{Sn: 0, Platform: "mock", User: &user.User{Id: "bot"}, Status: login.LoginStatusOnline, Adapter: "fixture"}
	if err := srv.Apply(&mockProvider{logins: []*login.Login{source}}); err != nil {
		t.Fatal(err)
	}
	publish := func(content string) error {
		return srv.Post(&event.Event{Type: event.EventTypeMessageCreated, Timestamp: 1, Login: source, Message: &message.Message{Id: content, Content: content}})
	}
	for n := 0; n < 4; n++ {
		if err := publish(strconv.Itoa(n)); err != nil {
			t.Fatal(err)
		}
	}
	httpServer := newTestHTTPServer(t, srv)
	defer httpServer.Close()
	conn, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(httpServer.URL, "http")+"/v1/events", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	conn.SetReadDeadline(time.Now().Add(3 * time.Second))
	zero := int64(0)
	if err := conn.WriteJSON(operation.Operation{Op: operation.OpcodeIdentify, Body: operation.IdentifyBody{Token: "fixture", Sn: &zero}}); err != nil {
		t.Fatal(err)
	}
	var frame struct {
		Op   operation.Opcode `json:"op"`
		Body json.RawMessage  `json:"body"`
	}
	if err := conn.ReadJSON(&frame); err != nil {
		t.Fatal(err)
	}
	if frame.Op != operation.OpcodeReady {
		t.Fatalf("ready=%d", frame.Op)
	}
	readSequence := func() int64 {
		t.Helper()
		if err := conn.ReadJSON(&frame); err != nil {
			t.Fatal(err)
		}
		if frame.Op != operation.OpcodeEvent {
			t.Fatalf("event=%d", frame.Op)
		}
		var body event.Event
		if err := json.Unmarshal(frame.Body, &body); err != nil {
			t.Fatal(err)
		}
		return body.Sn
	}
	if sn := readSequence(); sn != 1 {
		t.Fatalf("first replay=%d", sn)
	}
	sent := make(chan error, 1)
	go func() { sent <- publish("4") }()
	for _, want := range []int64{2, 3, 4} {
		if sn := readSequence(); sn != want {
			t.Fatalf("replay sequence=%d want=%d", sn, want)
		}
	}
	if err := <-sent; err != nil {
		t.Fatal(err)
	}
	const count = 12
	results := make(chan error, count)
	for n := 0; n < count; n++ {
		go func(n int) { results <- publish(fmt.Sprintf("parallel-%d", n)) }(n)
	}
	for want := int64(5); want < 5+count; want++ {
		if sn := readSequence(); sn != want {
			t.Fatalf("concurrent sequence=%d want=%d", sn, want)
		}
	}
	for n := 0; n < count; n++ {
		if err := <-results; err != nil {
			t.Fatal(err)
		}
	}
}
