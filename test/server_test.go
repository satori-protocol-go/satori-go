package testsuite

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/satori-protocol-go/satori-go/pkg/satori/model/event"
	"github.com/satori-protocol-go/satori-go/pkg/satori/model/login"
	"github.com/satori-protocol-go/satori-go/pkg/satori/model/operation"
	"github.com/satori-protocol-go/satori-go/pkg/satori/model/user"
	satoriserver "github.com/satori-protocol-go/satori-go/pkg/satori/server"
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
	err = server.Route(string(satoriserver.ApiMessageCreate), func(request satoriserver.Request[any]) (any, error) {
		called = true
		if request.Action != string(satoriserver.ApiMessageCreate) {
			t.Fatalf("unexpected action: %s", request.Action)
		}
		return map[string]any{
			"id":      "123",
			"content": "ok",
		}, nil
	})
	if err != nil {
		t.Fatalf("register route failed: %v", err)
	}

	httpServer := httptest.NewServer(server.Handler())
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

	err = server.Route("*", func(request satoriserver.Request[any]) (any, error) {
		return map[string]any{"action": request.Action}, nil
	})
	if err != nil {
		t.Fatalf("register internal wildcard failed: %v", err)
	}

	httpServer := httptest.NewServer(server.Handler())
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

	httpServer := httptest.NewServer(server.Handler())
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

	httpServer := httptest.NewServer(server.Handler())
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
