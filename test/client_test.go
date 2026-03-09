package testsuite

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"testing"
	"time"

	satoriclient "github.com/satori-protocol-go/satori-go/pkg/satori/client"
	clientnetwork "github.com/satori-protocol-go/satori-go/pkg/satori/client/network"
	"github.com/satori-protocol-go/satori-go/pkg/satori/model/event"
	"github.com/satori-protocol-go/satori-go/pkg/satori/model/login"
	"github.com/satori-protocol-go/satori-go/pkg/satori/model/message"
	"github.com/satori-protocol-go/satori-go/pkg/satori/model/user"
	satoriserver "github.com/satori-protocol-go/satori-go/pkg/satori/server"
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

	err = srv.Route(string(satoriserver.ApiMessageCreate), func(request satoriserver.Request[any]) (any, error) {
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
	if err != nil {
		t.Fatalf("route register failed: %v", err)
	}

	httpServer := httptest.NewServer(srv.Handler())
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

	httpServer := httptest.NewServer(srv.Handler())
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
