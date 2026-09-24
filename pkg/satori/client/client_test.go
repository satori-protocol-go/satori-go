package client_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	satoriclient "github.com/satori-protocol-go/satori-go/pkg/satori/client"
	clientnetwork "github.com/satori-protocol-go/satori-go/pkg/satori/client/network"
	"github.com/satori-protocol-go/satori-go/pkg/satori/model/event"
	"github.com/satori-protocol-go/satori-go/pkg/satori/model/login"
	"github.com/satori-protocol-go/satori-go/pkg/satori/model/message"
	"github.com/satori-protocol-go/satori-go/pkg/satori/model/operation"
	"github.com/satori-protocol-go/satori-go/pkg/satori/model/user"
	"github.com/satori-protocol-go/satori-go/pkg/satori/protocol"
	satoriserver "github.com/satori-protocol-go/satori-go/pkg/satori/server"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

type staticAPIConfig struct {
	base    string
	token   string
	timeout time.Duration
}

type customConfig struct {
	identity string
}

func (s staticAPIConfig) APIBase() string {
	return s.base
}

func (s staticAPIConfig) TokenValue() string {
	return s.token
}

func (s staticAPIConfig) TimeoutValue() time.Duration {
	return s.timeout
}

func (c customConfig) APIBase() string {
	return "http://localhost:5140/v1"
}

func (c customConfig) TokenValue() string {
	return ""
}

func (c customConfig) TimeoutValue() time.Duration {
	return 0
}

func (c customConfig) Identity() string {
	return c.identity
}

func (c customConfig) NetworkKind() string {
	return "custom"
}

type customRunner struct {
	id string
}

func (r *customRunner) ID() string {
	return r.id
}

func (r *customRunner) Run(context.Context) error {
	return nil
}

func (r *customRunner) Close() error {
	return nil
}

type clientMockProvider struct {
	logins []*login.Login
}

func (m *clientMockProvider) GetLogins(context.Context) ([]*login.Login, error) {
	return m.logins, nil
}

func (m *clientMockProvider) ProxyUrls() []string {
	return []string{"https://example.com"}
}

func (m *clientMockProvider) Ensure(platform string, selfID string) bool {
	return platform == "mock" && selfID == "bot"
}

func (m *clientMockProvider) HandleInternal(
	request satoriserver.Request[map[string]any],
	path string,
) (*satoriserver.Response, error) {
	_ = request
	_ = path
	return nil, satoriserver.NotFound("not found")
}

func (m *clientMockProvider) HandleProxied(prefix string, rawURL string) (*satoriserver.Response, error) {
	_ = prefix
	_ = rawURL
	return nil, nil
}

func TestClientAPIProtocolUploadAndDownload(t *testing.T) {
	srv, err := satoriserver.NewServer(satoriserver.Config{})
	if err != nil {
		t.Fatalf("new server failed: %v", err)
	}
	defer srv.Close()

	srv.Route(protocol.ApiMessageCreate, func(request *satoriserver.Request[any]) (any, error) {
		params, ok := request.Params.(map[string]any)
		if !ok {
			t.Fatalf("unexpected params type: %T", request.Params)
		}
		content, _ := params["content"].(string)
		return []*message.Message{{
			Id:      "m1",
			Content: content,
		}}, nil
	})

	httpServer := newTestHTTPServer(t, srv)
	defer httpServer.Close()

	loginInfo := &login.Login{
		Platform: "mock",
		User:     &user.User{Id: "bot"},
		Status:   login.LoginStatusOnline,
		Adapter:  "mock",
	}
	account := satoriclient.NewAccount(loginInfo, staticAPIConfig{base: httpServer.URL + "/v1"}, nil, nil)

	messages, err := account.Protocol.MessageCreate(context.Background(), "c1", "hello", nil)
	if err != nil {
		t.Fatalf("message create failed: %v", err)
	}
	if len(messages) != 1 || messages[0].Content != "hello" {
		t.Fatalf("unexpected message response: %#v", messages)
	}

	uploadResp, err := account.Protocol.UploadCreateNamed(context.Background(), map[string]satoriclient.Upload{
		"file": satoriclient.NewUpload([]byte("hello-upload"), "demo.txt", "text/plain"),
	})
	if err != nil {
		t.Fatalf("upload create failed: %v", err)
	}

	internalURL, ok := uploadResp["file"]
	if !ok || internalURL == "" {
		t.Fatalf("unexpected upload response: %#v", uploadResp)
	}

	data, err := account.Protocol.Download(context.Background(), internalURL)
	if err != nil {
		t.Fatalf("download failed: %v", err)
	}
	if string(data) != "hello-upload" {
		t.Fatalf("download content mismatch: %q", string(data))
	}
}

func TestClientAppWebSocketEventDispatch(t *testing.T) {
	srv, err := satoriserver.NewServer(satoriserver.Config{Token: "secret"})
	if err != nil {
		t.Fatalf("new server failed: %v", err)
	}
	defer srv.Close()

	provider := &clientMockProvider{logins: []*login.Login{{
		Sn:       1,
		Platform: "mock",
		User:     &user.User{Id: "bot"},
		Status:   login.LoginStatusOnline,
		Adapter:  "mock",
	}}}
	if err := srv.Apply(provider); err != nil {
		t.Fatalf("apply provider failed: %v", err)
	}

	httpServer := newTestHTTPServer(t, srv)
	defer httpServer.Close()

	host, port := parseHostPort(t, httpServer.URL)
	app, err := satoriclient.NewApp(satoriclient.WebSocketConfig{
		Host:    host,
		Port:    port,
		Version: "v1",
		Token:   "secret",
	})
	if err != nil {
		t.Fatalf("new app failed: %v", err)
	}

	gotEvent := make(chan *event.Event, 1)
	app.Register(func(account *satoriclient.Account, evt *event.Event) error {
		if account.SelfID() == "bot" && evt.Type == event.EventTypeMessageCreated {
			gotEvent <- evt
		}
		return nil
	})

	runCtx, cancel := context.WithCancel(context.Background())
	defer cancel()
	errCh := make(chan error, 1)
	go func() {
		errCh <- app.Run(runCtx)
	}()

	deadline := time.Now().Add(5 * time.Second)
	for {
		accounts := app.AccountsBySelfID("bot")
		if len(accounts) > 0 {
			break
		}
		if time.Now().After(deadline) {
			cancel()
			_ = <-errCh
			t.Fatal("timeout waiting for websocket account ready")
		}
		time.Sleep(50 * time.Millisecond)
	}

	err = srv.Post(&event.Event{
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

	select {
	case evt := <-gotEvent:
		if evt.Type != event.EventTypeMessageCreated {
			t.Fatalf("unexpected event type: %s", evt.Type)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timeout waiting for websocket event")
	}

	cancel()
	if err := <-errCh; err != nil {
		t.Fatalf("app run failed: %v", err)
	}
}

func TestClientRegisterNetworkFactory(t *testing.T) {
	app, err := satoriclient.NewApp()
	if err != nil {
		t.Fatalf("new app failed: %v", err)
	}

	err = app.RegisterNetworkFactory("custom", func(_ *satoriclient.App, cfg satoriclient.Config) (clientnetwork.Runner, satoriclient.APIConfig, error) {
		custom, ok := cfg.(customConfig)
		if !ok {
			t.Fatalf("unexpected config type: %T", cfg)
		}
		return &customRunner{id: "custom/" + custom.identity}, custom, nil
	})
	if err != nil {
		t.Fatalf("register network factory failed: %v", err)
	}

	if err := app.Apply(customConfig{identity: "demo"}); err != nil {
		t.Fatalf("apply custom config failed: %v", err)
	}
}

func parseHostPort(t *testing.T, rawURL string) (string, int) {
	t.Helper()
	parsed, err := url.Parse(rawURL)
	if err != nil {
		t.Fatalf("parse url failed: %v", err)
	}
	host, portRaw, err := net.SplitHostPort(parsed.Host)
	if err != nil {
		t.Fatalf("split host port failed: %v", err)
	}
	port, err := strconv.Atoi(portRaw)
	if err != nil {
		t.Fatalf("atoi port failed: %v", err)
	}
	if host == "" {
		host = "127.0.0.1"
	}
	return host, port
}

var _ = http.MethodGet

func TestClientInternalNormalizesActionCompat(t *testing.T) {
	var paths []string
	apiServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		paths = append(paths, request.URL.Path)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer apiServer.Close()

	account := satoriclient.NewAccount(
		&login.Login{
			Platform: "mock",
			User:     &user.User{Id: "bot"},
		},
		staticAPIConfig{base: apiServer.URL + "/v1", timeout: time.Second},
		nil,
		nil,
	)

	actions := []string{"ping", "internal/ping", "/internal/ping/"}
	for _, action := range actions {
		result, err := account.Protocol.Internal(context.Background(), action, http.MethodPost, map[string]any{"x": 1})
		if err != nil {
			t.Fatalf("internal call failed for %q: %v", action, err)
		}
		var resultMap map[string]any
		decodeErr := json.NewDecoder(result.Body).Decode(&resultMap)
		result.Body.Close()
		if decodeErr != nil || resultMap["ok"] != true {
			t.Fatalf("unexpected internal result for %q: %#v", action, result)
		}
	}

	for index, path := range paths {
		if path != "/v1/internal/ping" {
			t.Fatalf("path[%d] mismatch: got %q want %q", index, path, "/v1/internal/ping")
		}
	}
}

func TestClientWebhookNetworkRejectsUnauthorizedRequests(t *testing.T) {
	metaServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/v1/meta" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"logins":[],"proxy_urls":[]}`))
	}))
	defer metaServer.Close()

	metaHost, metaPort := parseHostPort(t, metaServer.URL)
	listenPort := freePort(t)
	app, err := satoriclient.NewApp(satoriclient.WebhookConfig{
		Host:       "127.0.0.1",
		Port:       listenPort,
		Path:       "/events",
		Token:      "secret",
		ServerHost: metaHost,
		ServerPort: metaPort,
		Version:    "v1",
		Timeout:    time.Second,
	})
	if err != nil {
		t.Fatalf("new app failed: %v", err)
	}

	runCtx, cancel := context.WithCancel(context.Background())
	defer cancel()
	errCh := make(chan error, 1)
	go func() {
		errCh <- app.Run(runCtx)
	}()

	waitCtx, waitCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer waitCancel()
	if err := app.WaitForAvailable(waitCtx); err != nil {
		cancel()
		_ = <-errCh
		t.Fatalf("wait for available failed: %v", err)
	}

	callbackURL := "http://127.0.0.1:" + strconv.Itoa(listenPort) + "/events"
	statusNoAuth := postWebhookEvent(t, callbackURL, "", []byte(`{"proxy_urls":[]}`))
	if statusNoAuth != http.StatusUnauthorized {
		t.Fatalf("missing auth status mismatch: got %d want %d", statusNoAuth, http.StatusUnauthorized)
	}

	statusWrongAuth := postWebhookEvent(t, callbackURL, "Bearer wrong", []byte(`{"proxy_urls":[]}`))
	if statusWrongAuth != http.StatusUnauthorized {
		t.Fatalf("wrong auth status mismatch: got %d want %d", statusWrongAuth, http.StatusUnauthorized)
	}

	statusValid := postWebhookEvent(t, callbackURL, "Bearer secret", []byte(`{"proxy_urls":[]}`))
	if statusValid != http.StatusOK {
		t.Fatalf("valid auth status mismatch: got %d want %d", statusValid, http.StatusOK)
	}

	cancel()
	if runErr := <-errCh; runErr != nil {
		t.Fatalf("app run failed: %v", runErr)
	}
}

func TestClientRequestInternalWithRawRequest(t *testing.T) {
	var (
		gotMethod string
		gotHeader string
		gotQuery  string
	)
	apiServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		gotMethod = request.Method
		gotHeader = request.Header.Get("X-Raw")
		gotQuery = request.URL.Query().Get("source")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":"yes"}`))
	}))
	defer apiServer.Close()

	account := satoriclient.NewAccount(
		&login.Login{Platform: "mock", User: &user.User{Id: "bot"}},
		staticAPIConfig{base: apiServer.URL + "/v1"},
		nil,
		nil,
	)

	result, err := account.Protocol.RequestInternal(
		context.Background(),
		apiServer.URL+"/echo",
		"",
		nil,
		satoriclient.WithRawRequest(func(request *http.Request) {
			request.Header.Set("X-Raw", "true")
			query := request.URL.Query()
			query.Set("source", "raw")
			request.URL.RawQuery = query.Encode()
		}),
	)
	if err != nil {
		t.Fatalf("request internal failed: %v", err)
	}
	if gotMethod != http.MethodGet {
		t.Fatalf("method mismatch: got %q want %q", gotMethod, http.MethodGet)
	}
	if gotHeader != "true" {
		t.Fatalf("header mismatch: got %q want %q", gotHeader, "true")
	}
	if gotQuery != "raw" {
		t.Fatalf("query mismatch: got %q want %q", gotQuery, "raw")
	}
	defer result.Body.Close()
	var resultMap map[string]string
	if err := json.NewDecoder(result.Body).Decode(&resultMap); err != nil {
		t.Fatal(err)
	}
	if resultMap["ok"] != "yes" {
		t.Fatalf("response mismatch: %#v", result)
	}
}

func TestAccountCustomWithOptions(t *testing.T) {
	self := &login.Login{
		Platform: "mock",
		User:     &user.User{Id: "bot"},
	}
	base := satoriclient.APIInfo{
		Host:    "localhost",
		Port:    5140,
		Path:    "",
		Version: "v1",
		Token:   "base-token",
		Timeout: 2 * time.Second,
	}
	account := satoriclient.NewAccount(self, base, nil, nil)

	customized := account.CustomWith(
		satoriclient.WithCustomHost("api.example.com"),
		satoriclient.WithCustomPort(7443),
		satoriclient.WithCustomPath("/bot"),
		satoriclient.WithCustomVersion("v2"),
		satoriclient.WithCustomSecure(true),
		satoriclient.WithCustomToken("next-token"),
		satoriclient.WithCustomTimeout(5*time.Second),
	)
	if customized.Config().APIBase() != "https://api.example.com:7443/bot/v2" {
		t.Fatalf("customized api base mismatch: %q", customized.Config().APIBase())
	}
	if customized.Config().TokenValue() != "next-token" {
		t.Fatalf("customized token mismatch: %q", customized.Config().TokenValue())
	}
	if customized.Config().TimeoutValue() != 5*time.Second {
		t.Fatalf("customized timeout mismatch: %v", customized.Config().TimeoutValue())
	}
	if account.Config().APIBase() != "http://localhost:5140/v1" {
		t.Fatalf("original account config should remain unchanged: %q", account.Config().APIBase())
	}

	accountFromStatic := satoriclient.NewAccount(
		self,
		staticAPIConfig{
			base:    "https://static.example.com:9443/base/v9",
			token:   "static-token",
			timeout: 3 * time.Second,
		},
		nil,
		nil,
	)
	parsed := accountFromStatic.CustomWith(
		satoriclient.WithCustomPath("/next"),
		satoriclient.WithCustomVersion("v1"),
	)
	if parsed.Config().APIBase() != "https://static.example.com:9443/next/v1" {
		t.Fatalf("parsed custom api base mismatch: %q", parsed.Config().APIBase())
	}
}

func postWebhookEvent(t *testing.T, endpoint string, authorization string, payload []byte) int {
	t.Helper()
	request, err := http.NewRequest(http.MethodPost, endpoint, bytes.NewReader(payload))
	if err != nil {
		t.Fatalf("new request failed: %v", err)
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set(protocol.HeaderOpcode, strconv.Itoa(int(operation.OpcodeMeta)))
	if strings.TrimSpace(authorization) != "" {
		request.Header.Set("Authorization", authorization)
	}
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatalf("post webhook request failed: %v", err)
	}
	defer response.Body.Close()
	_, _ = io.Copy(io.Discard, response.Body)
	return response.StatusCode
}

func freePort(t *testing.T) int {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen free port failed: %v", err)
	}
	defer listener.Close()
	_, rawPort, err := net.SplitHostPort(listener.Addr().String())
	if err != nil {
		t.Fatalf("split host port failed: %v", err)
	}
	value, err := strconv.Atoi(rawPort)
	if err != nil {
		t.Fatalf("parse free port failed: %v", err)
	}
	return value
}

func newTestHTTPServer(t *testing.T, srv *satoriserver.Server) *httptest.Server {
	t.Helper()
	handler, err := srv.Handler()
	if err != nil {
		t.Fatal(err)
	}
	return httptest.NewServer(handler)
}

func TestNativeHTTPResponse(t *testing.T) {
	srv, err := satoriserver.NewServer(satoriserver.Config{Token: "api-token"})
	if err != nil {
		t.Fatal(err)
	}
	defer srv.Close()
	srv.Route(protocol.ParseApi("internal/*"), func(r *satoriserver.Request[any]) (any, error) {
		body, err := io.ReadAll(r.Origin.Body)
		if err != nil {
			return nil, err
		}
		if r.Origin.Method != "PATCH" || r.Origin.URL.Query().Get("cursor") != " token+/== " || string(body) != "\x00\x01payload" {
			t.Errorf("native request=%s %s %q", r.Origin.Method, r.Origin.URL, body)
		}
		status, _ := strconv.Atoi(r.Origin.URL.Query().Get("status"))
		response := satoriserver.NewResponse(status, body)
		if status == 201 {
			response = satoriserver.NewResponse(status, []byte(`[{"id":"native"}]`))
		}
		if status == 204 {
			response = satoriserver.NewResponse(status, nil)
		}
		response.Header.Set("Content-Type", r.Origin.Header.Get("Content-Type"))
		response.Header.Set("X-Platform-Trace", "trace")
		return response, nil
	})
	httpServer := newTestHTTPServer(t, srv)
	defer httpServer.Close()
	account := satoriclient.NewAccount(&login.Login{Platform: "mock", User: &user.User{Id: "bot"}}, staticAPIConfig{base: httpServer.URL + "/v1", token: "api-token"}, nil, nil)
	for _, status := range []int{200, 201, 204, 422} {
		endpoint := httpServer.URL + "/v1/internal/fixture?" + url.Values{"cursor": {" token+/== "}, "status": {strconv.Itoa(status)}}.Encode()
		response, err := account.RequestInternal(context.Background(), endpoint, "PATCH", nil, satoriclient.WithRequestBody(bytes.NewReader([]byte{0, 1, 'p', 'a', 'y', 'l', 'o', 'a', 'd'}), "application/octet-stream"), satoriclient.WithRequestTimeout(time.Second))
		if err != nil {
			t.Fatal(err)
		}
		body, readErr := io.ReadAll(response.Body)
		response.Body.Close()
		if readErr != nil || response.StatusCode != status || response.Header.Get("X-Platform-Trace") != "trace" {
			t.Fatalf("native response status=%d body=%q err=%v", response.StatusCode, body, readErr)
		}
		expected := "\x00\x01payload"
		if status == 201 {
			expected = `[{"id":"native"}]`
		}
		if status == 204 {
			expected = ""
		}
		if string(body) != expected {
			t.Fatalf("native body=%q", body)
		}
	}
}

type lifecycleRunner struct {
	started chan struct{}
	closed  chan struct{}
	mode    string
	failure error
	once    sync.Once
}

func (r *lifecycleRunner) ID() string { return "lifecycle/fixture" }
func (r *lifecycleRunner) Run(ctx context.Context) error {
	close(r.started)
	switch r.mode {
	case "normal":
		return nil
	case "failure":
		return r.failure
	default:
		<-ctx.Done()
		return ctx.Err()
	}
}
func (r *lifecycleRunner) Close() error { r.once.Do(func() { close(r.closed) }); return nil }

func TestApplicationLifecycle(t *testing.T) {
	for _, mode := range []string{"normal", "failure", "close", "cancel"} {
		t.Run(mode, func(t *testing.T) {
			failure := errors.New("fixture runner failure")
			runner := &lifecycleRunner{started: make(chan struct{}), closed: make(chan struct{}), mode: mode, failure: failure}
			app, err := satoriclient.NewApp()
			if err != nil {
				t.Fatal(err)
			}
			if err := app.RegisterNetworkFactory("custom", func(*satoriclient.App, satoriclient.Config) (clientnetwork.Runner, satoriclient.APIConfig, error) {
				return runner, staticAPIConfig{base: "http://127.0.0.1"}, nil
			}); err != nil {
				t.Fatal(err)
			}
			if err := app.Apply(customConfig{identity: mode}); err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			done := app.RunAsync(ctx)
			select {
			case <-runner.started:
			case <-time.After(time.Second):
				t.Fatal("runner startup timeout")
			}
			if mode == "close" {
				if err := app.Close(); err != nil {
					t.Fatal(err)
				}
			}
			if mode == "cancel" {
				cancel()
			}
			select {
			case err := <-done:
				if mode == "failure" && !errors.Is(err, failure) {
					t.Fatalf("failure=%v", err)
				}
				if mode != "failure" && err != nil {
					t.Fatal(err)
				}
			case <-time.After(2 * time.Second):
				t.Fatal("Run completion timeout")
			}
			select {
			case <-runner.closed:
			case <-time.After(time.Second):
				t.Fatal("runner cleanup timeout")
			}
		})
	}
}

func TestLoginStateSynchronization(t *testing.T) {
	app, err := satoriclient.NewApp()
	if err != nil {
		t.Fatal(err)
	}
	config := staticAPIConfig{base: "http://127.0.0.1/v1", token: "fixture"}
	first := &login.Login{Sn: 0, Platform: "mock", User: &user.User{Id: "alpha", Name: "before"}, Status: login.LoginStatusOnline, Adapter: "fixture", Features: []string{"old"}}
	partial := &login.Login{Sn: 1, Status: login.LoginStatusOffline, Adapter: "fixture"}
	second := &login.Login{Sn: 0, Platform: "mock", User: &user.User{Id: "beta"}, Status: login.LoginStatusOnline, Adapter: "fixture"}
	if err := app.SyncLogins("source-a", config, nil, []*login.Login{first, partial}); err != nil {
		t.Fatal(err)
	}
	if err := app.SyncLogins("source-b", config, nil, []*login.Login{second}); err != nil {
		t.Fatal(err)
	}
	if len(app.Accounts()) != 3 {
		t.Fatalf("login count=%d", len(app.Accounts()))
	}
	alpha := app.AccountsBySelfID("alpha")[0]
	var change event.Event
	if err := json.Unmarshal([]byte(`{"sn":1,"type":"login-updated","timestamp":1,"login":{"sn":0,"status":0,"user":{"id":"alpha","name":"after"},"features":["new"]}}`), &change); err != nil {
		t.Fatal(err)
	}
	if err := app.PostEvent("source-a", &change); err != nil {
		t.Fatal(err)
	}
	info := alpha.SelfInfo()
	if alpha.Connected() || info.User.Name != "after" || info.Status != login.LoginStatusOffline || len(info.Features) != 1 || info.Features[0] != "new" {
		t.Fatalf("updated login=%+v", info)
	}
	// The snapshot can be inspected independently while the live account advances.
	info.User.Name = "caller-owned"
	if alpha.SelfInfo().User.Name != "after" {
		t.Fatalf("live login name=%s", alpha.SelfInfo().User.Name)
	}
	app.UpdateProxyURLs("source-a", []string{"https://example.invalid/assets/"})
	if len(app.Accounts()) != 3 || len(alpha.ProxyURLs()) != 1 {
		t.Fatalf("META logins=%d proxies=%v", len(app.Accounts()), alpha.ProxyURLs())
	}
	if err := app.SyncLogins("source-a", config, nil, []*login.Login{}); err != nil {
		t.Fatal(err)
	}
	if len(app.Accounts()) != 1 || len(app.AccountsBySelfID("beta")) != 1 {
		t.Fatalf("empty READY login set=%v", app.Accounts())
	}
	if err := json.Unmarshal([]byte(`{"sn":2,"type":"login-removed","timestamp":2,"login":{"sn":0,"status":0}}`), &change); err != nil {
		t.Fatal(err)
	}
	if err := app.PostEvent("source-b", &change); err != nil {
		t.Fatal(err)
	}
	if len(app.Accounts()) != 0 {
		t.Fatalf("removed source login set=%v", app.Accounts())
	}
	first.Sn = 7
	if err := app.SyncLogins("source-a", config, nil, []*login.Login{first}); err != nil {
		t.Fatal(err)
	}
	received := ""
	app.Register(func(account *satoriclient.Account, evt *event.Event) error { received = account.SelfID(); return nil })
	if err := app.PostEvent("source-a", &event.Event{Type: event.EventTypeMessageCreated, Login: &login.Login{Sn: 7, Platform: "mock", User: &user.User{Id: "alpha"}}}); err != nil {
		t.Fatal(err)
	}
	if received != "alpha" || app.AccountsBySelfID("alpha")[0].SelfInfo().Sn != 7 {
		t.Fatalf("rebound account=%s", received)
	}
}

func TestAccountSnapshotConsistency(t *testing.T) {
	app, err := satoriclient.NewApp()
	if err != nil {
		t.Fatal(err)
	}
	info := &login.Login{Sn: 0, Platform: "mock", User: &user.User{Id: "bot", Name: "0"}, Status: login.LoginStatusOnline, Features: []string{"0"}}
	if err := app.SyncLogins("source", staticAPIConfig{base: "http://127.0.0.1"}, nil, []*login.Login{info}); err != nil {
		t.Fatal(err)
	}
	account := app.AccountsBySelfID("bot")[0]
	done := make(chan error, 1)
	go func() {
		for n := 1; n <= 100; n++ {
			value := strconv.Itoa(n)
			err := app.PostEvent("source", &event.Event{Type: event.EventTypeLoginUpdated, Login: &login.Login{Sn: 0, Platform: "mock", User: &user.User{Id: "bot", Name: value}, Status: login.LoginStatusOnline, Features: []string{value}}})
			if err != nil {
				done <- err
				return
			}
		}
		done <- nil
	}()
	for n := 0; n < 100; n++ {
		snapshot := account.SelfInfo()
		if len(snapshot.Features) != 1 || snapshot.User.Name != snapshot.Features[0] {
			t.Fatalf("inconsistent login=%+v", snapshot)
		}
		_ = account.Config().APIBase()
	}
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if account.SelfInfo().User.Name != "100" {
		t.Fatalf("final login=%+v", account.SelfInfo())
	}
}
