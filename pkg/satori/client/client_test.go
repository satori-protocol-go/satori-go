package client_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	satoriclient "github.com/satori-protocol-go/satori-go/pkg/satori/client"
	clientnetwork "github.com/satori-protocol-go/satori-go/pkg/satori/client/network"
	"github.com/satori-protocol-go/satori-go/pkg/satori/logging"
	"github.com/satori-protocol-go/satori-go/pkg/satori/model/event"
	"github.com/satori-protocol-go/satori-go/pkg/satori/model/login"
	"github.com/satori-protocol-go/satori-go/pkg/satori/model/message"
	"github.com/satori-protocol-go/satori-go/pkg/satori/model/user"
	"github.com/satori-protocol-go/satori-go/pkg/satori/protocol"
	satoriserver "github.com/satori-protocol-go/satori-go/pkg/satori/server"
	"io"
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

func (m *clientMockProvider) HandleProxied(ctx context.Context, prefix string, rawURL string) (*satoriserver.Response, error) {
	_ = prefix
	_ = rawURL
	return nil, nil
}

func TestClientHTTP(t *testing.T) {
	srv, err := satoriserver.NewServer(satoriserver.Config{Token: "fixture"})
	if err != nil {
		t.Fatalf("new server failed: %v", err)
	}
	defer srv.Close()
	if err := srv.Apply(&clientMockProvider{}); err != nil {
		t.Fatal(err)
	}

	srv.Route(protocol.ApiMessageCreate, func(request *satoriserver.Request[any]) (any, error) {
		params, ok := request.Params.(map[string]any)
		if !ok {
			t.Fatalf("unexpected params type: %T", request.Params)
		}
		content, _ := params["content"].(string)
		return []*message.Message{{
			Id:       "m1",
			Content:  content,
			Referrer: map[string]any{"channel": params["channel_id"]},
		}}, nil
	})

	srv.Route(protocol.ApiMessageList, func(r *satoriserver.Request[any]) (any, error) {
		params := r.Params.(map[string]any)
		id := "platform-default"
		if value, ok := params["limit"]; ok {
			id = value.(json.Number).String()
		}
		return map[string]any{"data": []*message.Message{{Id: id}}}, nil
	})
	srv.Route(protocol.ApiUserGet, func(r *satoriserver.Request[any]) (any, error) {
		return satoriserver.NewResponse(403, []byte("  native error\n")), nil
	})
	httpServer := newTestHTTPServer(t, srv)
	defer httpServer.Close()

	loginInfo := &login.Login{
		Platform: "mock",
		User:     &user.User{Id: "bot"},
		Status:   login.LoginStatusOnline,
		Adapter:  "mock",
	}

	var mu sync.Mutex
	var entries []string
	logger := clientLogFunc(func(_ context.Context, level logging.Level, v ...any) {
		mu.Lock()
		defer mu.Unlock()
		entries = append(entries, string(level)+" "+fmt.Sprint(v...))
	})
	app, err := satoriclient.NewApp()
	if err != nil {
		t.Fatal(err)
	}
	app.RegisterLogger(logger)
	cfg := staticAPIConfig{base: httpServer.URL + "/v1", token: "fixture"}
	if err := app.SyncLogins("source", cfg, nil, []*login.Login{loginInfo}); err != nil {
		t.Fatal(err)
	}
	account := app.AccountsBySelfID("bot")[0]

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
	for _, limit := range []int{0, 123} {
		page, err := account.Protocol.MessageList(context.Background(), "c", "", "", limit, "")
		want := "platform-default"
		if limit != 0 {
			want = strconv.Itoa(limit)
		}
		if err != nil || len(page.Data) != 1 || page.Data[0].Id != want {
			t.Fatalf("platform default result=%+v error=%v", page, err)
		}
	}
	sent, err := account.Protocol.SendMessage(context.Background(), " c ", "literal", nil)
	if err != nil || len(sent) != 1 || sent[0].Referrer["channel"] != " c " {
		t.Fatalf("opaque channel=%+v error=%v", sent, err)
	}
	_, err = account.Protocol.UserGet(context.Background(), "u")
	var requestError satoriclient.SatoriError
	if !errors.As(err, &requestError) || requestError.ResponseBody() != "  native error\n" {
		t.Fatalf("error body=%v", err)
	}

	standalone := satoriclient.NewAccount(loginInfo, staticAPIConfig{base: httpServer.URL + "/v1", token: "original"}, nil, nil).CustomWith(satoriclient.WithCustomToken("fixture"))
	standalone.RegisterLogger(logger)
	if sent, err := standalone.MessageCreate(context.Background(), "custom", "configured", nil); err != nil || len(sent) != 1 || sent[0].Content != "configured" {
		t.Fatalf("custom request=%v error=%v", sent, err)
	}
	mu.Lock()
	text := strings.Join(entries, "\n")
	mu.Unlock()
	for _, entry := range []string{"info Login for ", "debug The Satori HTTP POST request returned HTTP 200", "warn The Satori HTTP POST request returned HTTP 403"} {
		if !strings.Contains(text, entry) {
			t.Fatalf("injected logs missing %q: %s", entry, text)
		}
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

func TestClientNativeTransport(t *testing.T) {
	srv, err := satoriserver.NewServer(satoriserver.Config{Token: "api-token"})
	if err != nil {
		t.Fatal(err)
	}
	defer srv.Close()
	release := make(chan struct{})
	defer func() {
		select {
		case <-release:
		default:
			close(release)
		}
	}()
	srv.Route(protocol.ParseApi("internal/*"), func(r *satoriserver.Request[any]) (any, error) {
		if r.Action == "internal/ping" {
			return map[string]string{"action": r.Action}, nil
		}
		if r.Action == "internal/stream" {
			reader, writer := io.Pipe()
			go func() {
				defer writer.Close()
				if _, err := writer.Write([]byte("first\n")); err != nil {
					return
				}
				select {
				case <-release:
				case <-r.Origin.Context().Done():
					return
				}
				writer.Write([]byte("second\n"))
			}()
			response := satoriserver.NewStreamResponse(200, reader)
			response.Header.Set("Content-Type", "text/event-stream")
			return response, nil
		}
		body, err := io.ReadAll(r.Origin.Body)
		if err != nil {
			return nil, err
		}
		if r.Origin.Method != "PATCH" || r.Origin.URL.Query().Get("cursor") != " token+/== " || r.Origin.Header.Get("X-Raw") != "enabled" || string(body) != "\x00\x01payload" {
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
		response, err := account.RequestInternal(context.Background(), endpoint, "PATCH", nil, satoriclient.WithRequestBody(bytes.NewReader([]byte{0, 1, 'p', 'a', 'y', 'l', 'o', 'a', 'd'}), "application/octet-stream"), satoriclient.WithRequestTimeout(time.Second), satoriclient.WithRawRequest(func(r *http.Request) { r.Header.Set("X-Raw", "enabled") }))
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

	internal, err := account.Internal(context.Background(), "ping", "GET", nil)
	if err != nil {
		t.Fatal(err)
	}
	var echo map[string]string
	decodeErr := json.NewDecoder(internal.Body).Decode(&echo)
	internal.Body.Close()
	if decodeErr != nil || echo["action"] != "internal/ping" {
		t.Fatalf("native action=%v error=%v", echo, decodeErr)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	streamed, err := account.RequestInternal(ctx, httpServer.URL+"/v1/internal/stream", "GET", nil)
	// This test uses the ordinary native route rather than a resource provider.
	if err != nil {
		t.Fatal(err)
	}
	defer streamed.Body.Close()
	first := make([]byte, 6)
	if _, err := io.ReadFull(streamed.Body, first); err != nil || string(first) != "first\n" {
		t.Fatalf("first streamed bytes=%q error=%v", first, err)
	}
	close(release)
	rest, err := io.ReadAll(streamed.Body)
	if err != nil || string(rest) != "second\n" {
		t.Fatalf("second streamed bytes=%q error=%v", rest, err)
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
	case "shutdown-failure":
		<-ctx.Done()
		return r.failure
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
	for _, mode := range []string{"normal", "failure", "close", "cancel", "shutdown-failure"} {
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
			if mode == "cancel" || mode == "shutdown-failure" {
				cancel()
			}
			select {
			case err := <-done:
				if (mode == "failure" || mode == "shutdown-failure") && !errors.Is(err, failure) {
					t.Fatalf("failure=%v", err)
				}
				if mode != "failure" && mode != "shutdown-failure" && err != nil {
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

func TestClientLogins(t *testing.T) {
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
	recovered := ""
	app.Register(func(account *satoriclient.Account, evt *event.Event) error {
		if evt.Sn == 99 {
			recovered = account.SelfID()
			if evt.Login.Status != login.LoginStatusOnline {
				t.Error("historical event login is not online")
			}
		}
		return nil
	})
	if err := app.PostEvent("source-b", &event.Event{Sn: 99, Type: event.EventTypeMessageCreated, Login: second, Message: &message.Message{Id: "historical"}}); err != nil {
		t.Fatal(err)
	}
	if recovered != "beta" {
		t.Fatalf("recovered account=%s", recovered)
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

	callbackErr := errors.New("fixture callback failed")
	app.Register(func(*satoriclient.Account, *event.Event) error { return callbackErr })
	err = app.PostEvent("source-a", &event.Event{Sn: 100, Type: event.EventTypeMessageCreated, Login: first, Message: &message.Message{Id: "callback"}})
	if !errors.Is(err, callbackErr) {
		t.Fatalf("callback result=%v", err)
	}
}

type clientLogFunc func(context.Context, logging.Level, ...any)

func (f clientLogFunc) Log(ctx context.Context, level logging.Level, values ...any) {
	f(ctx, level, values...)
}
