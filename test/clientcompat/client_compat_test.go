package clientcompat

import (
	"bytes"
	"context"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"
	"time"

	satoriclient "github.com/satori-protocol-go/satori-go/pkg/satori/client"
	"github.com/satori-protocol-go/satori-go/pkg/satori/model/login"
	"github.com/satori-protocol-go/satori-go/pkg/satori/model/operation"
	"github.com/satori-protocol-go/satori-go/pkg/satori/model/user"
	"github.com/satori-protocol-go/satori-go/pkg/satori/protocol"
)

type staticAPIConfig struct {
	base    string
	token   string
	timeout time.Duration
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
		resultMap, ok := result.(map[string]any)
		if !ok || resultMap["ok"] != true {
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
	if result["ok"] != "yes" {
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
	if customized.Config.APIBase() != "https://api.example.com:7443/bot/v2" {
		t.Fatalf("customized api base mismatch: %q", customized.Config.APIBase())
	}
	if customized.Config.TokenValue() != "next-token" {
		t.Fatalf("customized token mismatch: %q", customized.Config.TokenValue())
	}
	if customized.Config.TimeoutValue() != 5*time.Second {
		t.Fatalf("customized timeout mismatch: %v", customized.Config.TimeoutValue())
	}
	if account.Config.APIBase() != "http://localhost:5140/v1" {
		t.Fatalf("original account config should remain unchanged: %q", account.Config.APIBase())
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
	if parsed.Config.APIBase() != "https://static.example.com:9443/next/v1" {
		t.Fatalf("parsed custom api base mismatch: %q", parsed.Config.APIBase())
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
