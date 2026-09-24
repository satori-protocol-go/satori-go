package satori_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"github.com/satori-protocol-go/satori-go/pkg/satori/logging"
	"github.com/satori-protocol-go/satori-go/pkg/satori/protocol"
	"io"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	satoriadapter "github.com/satori-protocol-go/satori-go/pkg/satori/adapter/satori"
	"github.com/satori-protocol-go/satori-go/pkg/satori/model/event"
	"github.com/satori-protocol-go/satori-go/pkg/satori/model/login"
	"github.com/satori-protocol-go/satori-go/pkg/satori/model/message"
	"github.com/satori-protocol-go/satori-go/pkg/satori/model/operation"
	"github.com/satori-protocol-go/satori-go/pkg/satori/model/user"
	satoriserver "github.com/satori-protocol-go/satori-go/pkg/satori/server"
)

type satoriRemoteProvider struct {
	login *login.Login
}

func (p *satoriRemoteProvider) GetLogins(context.Context) ([]*login.Login, error) {
	if p.login == nil {
		return []*login.Login{}, nil
	}
	return []*login.Login{p.login}, nil
}

func (p *satoriRemoteProvider) ProxyUrls() []string {
	return []string{"https://example.com"}
}

func (p *satoriRemoteProvider) Ensure(platform string, selfID string) bool {
	if p.login == nil || p.login.User == nil {
		return false
	}
	return p.login.Platform == platform && p.login.User.Id == selfID
}

func (p *satoriRemoteProvider) HandleInternal(
	request satoriserver.Request[map[string]any],
	path string,
) (*satoriserver.Response, error) {
	_ = request
	_ = path
	return nil, satoriserver.NotFound("not found")
}

func (p *satoriRemoteProvider) HandleProxied(ctx context.Context, prefix string, rawURL string) (*satoriserver.Response, error) {
	_ = prefix
	_ = rawURL
	return nil, nil
}

func TestSatoriBridge(t *testing.T) {
	remotePort := findFreePort(t)
	localPort := findFreePort(t)

	remoteServer, err := satoriserver.NewServer(satoriserver.Config{
		Host:  "127.0.0.1",
		Port:  remotePort,
		Token: "remote-secret",
	})
	if err != nil {
		t.Fatalf("new remote server failed: %v", err)
	}
	defer remoteServer.Close()

	remoteLogin := &login.Login{
		Sn:       1,
		Platform: "satori",
		User:     &user.User{Id: "bot"},
		Status:   login.LoginStatusOnline,
		Adapter:  "satori",
	}
	if err := remoteServer.Apply(&satoriRemoteProvider{login: remoteLogin}); err != nil {
		t.Fatalf("apply remote provider failed: %v", err)
	}
	if err := remoteServer.Apply(&satoriRemoteProvider{login: &login.Login{Sn: 1, Platform: "satori", User: &user.User{Id: "second"}, Status: login.LoginStatusOnline, Adapter: "fixture"}}); err != nil {
		t.Fatal(err)
	}
	remoteServer.Route(protocol.ParseApi("internal/*"), func(r *satoriserver.Request[any]) (any, error) {
		body, err := io.ReadAll(r.Origin.Body)
		if err != nil {
			return nil, err
		}
		if r.Origin.URL.Query().Get("cursor") != " +/=" || r.SelfID != "second" {
			t.Errorf("native identity/query=%s %s", r.SelfID, r.Origin.URL.RawQuery)
		}
		switch r.Action {
		case "internal/binary":
			if r.Origin.Method != "PATCH" || r.Origin.Header.Get("Content-Type") != "application/octet-stream" || r.Origin.Header.Get("X-Native-Feature") != "original" {
				t.Errorf("native method/type=%s %s", r.Origin.Method, r.Origin.Header.Get("Content-Type"))
			}
			out := satoriserver.NewResponse(206, body)
			out.Header.Set("Content-Type", "application/octet-stream")
			out.Header.Set("X-Fixture", "native")
			return out, nil
		case "internal/empty":
			return satoriserver.NewResponse(204, nil), nil
		case "internal/array":
			return satoriserver.NewResponse(200, []byte(`[{"id":9007199254740993}]`)), nil
		default:
			return satoriserver.NewResponse(403, []byte(`{"reason":"fixture"}`)), nil
		}
	})
	remoteServer.Route(protocol.ApiMessageCreate, func(request *satoriserver.Request[any]) (any, error) {
		params, ok := request.Params.(map[string]any)
		if !ok {
			return nil, satoriserver.BadRequest("invalid params")
		}
		content, _ := params["content"].(string)
		return []*message.Message{{Id: request.SelfID, Content: content}}, nil
	})

	remoteCtx, remoteCancel := context.WithCancel(context.Background())
	defer remoteCancel()
	remoteErr := make(chan error, 1)
	go func() {
		remoteErr <- remoteServer.Run(remoteCtx)
	}()
	waitHTTPReady(t, fmt.Sprintf("http://127.0.0.1:%d/v1/meta", remotePort))

	var logMu sync.Mutex
	var logs []string
	logger := bridgeLogFunc(func(_ context.Context, level logging.Level, v ...any) {
		logMu.Lock()
		defer logMu.Unlock()
		logs = append(logs, string(level)+" "+fmt.Sprint(v...))
	})
	adapter, err := satoriadapter.New(satoriadapter.Config{
		Logger: logger,
		Host:   "127.0.0.1",
		Port:   remotePort,
		Token:  "remote-secret",
	})
	if err != nil {
		t.Fatalf("new satori adapter failed: %v", err)
	}

	localServer, err := satoriserver.NewServer(satoriserver.Config{
		Host:  "127.0.0.1",
		Port:  localPort,
		Token: "local-secret",
	})
	if err != nil {
		t.Fatalf("new local server failed: %v", err)
	}
	defer localServer.Close()

	if err := localServer.Apply(adapter); err != nil {
		t.Fatalf("apply adapter failed: %v", err)
	}

	localCtx, localCancel := context.WithCancel(context.Background())
	defer localCancel()
	localErr := make(chan error, 1)
	go func() {
		localErr <- localServer.Run(localCtx)
	}()
	waitHTTPReady(t, fmt.Sprintf("http://127.0.0.1:%d/v1/meta", localPort))
	waitAdapterReady(t, adapter)

	logins, err := adapter.GetLogins(context.Background())
	if err != nil || len(logins) != 2 || logins[0].Sn == logins[1].Sn {
		t.Fatalf("bridge logins=%+v err=%v", logins, err)
	}
	for _, id := range []string{"bot", "second"} {
		req, _ := http.NewRequest("POST", fmt.Sprintf("http://127.0.0.1:%d/v1/message.create", localPort), bytes.NewBufferString(`{"channel_id":"c","content":"hi"}`))
		req.Header.Set("Authorization", "Bearer local-secret")
		req.Header.Set("Satori-Platform", "satori")
		req.Header.Set("Satori-User-ID", id)
		req.Header.Set("Content-Type", "application/json")
		response, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		var result []message.Message
		err = json.NewDecoder(response.Body).Decode(&result)
		response.Body.Close()
		if err != nil || response.StatusCode != 200 || len(result) != 1 || result[0].Id != id {
			t.Fatalf("route %s => %+v %d err=%v", id, result, response.StatusCode, err)
		}
	}
	for _, tc := range []struct {
		action, method string
		status         int
		body           string
	}{
		{"binary", "PATCH", 206, "binary-bytes"},
		{"empty", "DELETE", 204, ""},
		{"array", "GET", 200, `[{"id":9007199254740993}]`},
		{"failure", "POST", 403, `{"reason":"fixture"}`},
	} {
		req, _ := http.NewRequest(tc.method, fmt.Sprintf("http://127.0.0.1:%d/v1/internal/%s?cursor=%%20%%2B%%2F%%3D", localPort, tc.action), bytes.NewBufferString(tc.body))
		req.Header.Set("Content-Type", "application/octet-stream")
		req.Header.Set("X-Native-Feature", "original")
		req.Header.Set("Authorization", "Bearer local-secret")
		req.Header.Set("Satori-Platform", "satori")
		req.Header.Set("Satori-User-ID", "second")
		response, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		raw, err := io.ReadAll(response.Body)
		response.Body.Close()
		if err != nil || response.StatusCode != tc.status || string(raw) != tc.body {
			t.Fatalf("native %s status=%d body=%q err=%v", tc.action, response.StatusCode, raw, err)
		}
	}

	connection, _, err := websocket.DefaultDialer.Dial(fmt.Sprintf("ws://127.0.0.1:%d/v1/events", localPort), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer connection.Close()
	connection.SetReadDeadline(time.Now().Add(3 * time.Second))
	if err := connection.WriteJSON(operation.Operation{Op: operation.OpcodeIdentify, Body: operation.IdentifyBody{Token: "local-secret"}}); err != nil {
		t.Fatal(err)
	}
	var frame struct {
		Op   operation.Opcode `json:"op"`
		Body json.RawMessage  `json:"body"`
	}
	if err := connection.ReadJSON(&frame); err != nil || frame.Op != operation.OpcodeReady {
		t.Fatalf("bridge ready=%d error=%v", frame.Op, err)
	}
	adapter.RegisterLogger(logger)
	for _, id := range []string{"bot", "second"} {
		source := &login.Login{Sn: 1, Platform: "satori", User: &user.User{Id: id}, Status: login.LoginStatusOnline, Adapter: "fixture"}
		if err := remoteServer.Post(&event.Event{Type: event.EventTypeMessageCreated, Timestamp: 1, Login: source, Message: &message.Message{Id: "event-" + id, Content: "forwarded"}}); err != nil {
			t.Fatal(err)
		}
		if err := connection.ReadJSON(&frame); err != nil {
			t.Fatal(err)
		}
		var evt event.Event
		if err := json.Unmarshal(frame.Body, &evt); err != nil {
			t.Fatal(err)
		}
		if frame.Op != operation.OpcodeEvent || evt.Login.User.Id != id || evt.Message.Id != "event-"+id || evt.Message.Content != "forwarded" {
			t.Fatalf("forwarded event=%+v", evt)
		}
	}

	logMu.Lock()
	output := strings.Join(logs, "\n")
	logMu.Unlock()
	if !strings.Contains(output, "Satori websocket ready") || !strings.Contains(output, "Satori HTTP method=") {
		t.Fatalf("bridge logger=%s", output)
	}
	localCancel()
	remoteCancel()
	waitRunExit(t, localErr)
	waitRunExit(t, remoteErr)
}

func waitAdapterReady(t *testing.T, adapter *satoriadapter.Adapter) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for {
		logins, err := adapter.GetLogins(context.Background())
		if err == nil && len(logins) > 0 {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("timeout waiting adapter login ready")
		}
		time.Sleep(50 * time.Millisecond)
	}
}

func waitHTTPReady(t *testing.T, endpoint string) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for {
		request, _ := http.NewRequest(http.MethodPost, endpoint, bytes.NewBufferString("{}"))
		request.Header.Set("Content-Type", "application/json")
		response, err := http.DefaultClient.Do(request)
		if err == nil {
			_ = response.Body.Close()
			if response.StatusCode >= 200 && response.StatusCode < 500 {
				return
			}
		}
		if time.Now().After(deadline) {
			t.Fatalf("timeout waiting server ready: %s", endpoint)
		}
		time.Sleep(50 * time.Millisecond)
	}
}

func waitRunExit(t *testing.T, errCh <-chan error) {
	t.Helper()
	select {
	case err := <-errCh:
		if err != nil {
			t.Fatalf("run returned error: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timeout waiting run exit")
	}
}

func findFreePort(t *testing.T) int {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen failed: %v", err)
	}
	defer listener.Close()
	_, portRaw, err := net.SplitHostPort(listener.Addr().String())
	if err != nil {
		t.Fatalf("split host port failed: %v", err)
	}
	port, err := strconv.Atoi(portRaw)
	if err != nil {
		t.Fatalf("atoi port failed: %v", err)
	}
	return port
}

type bridgeLogFunc func(context.Context, logging.Level, ...any)

func (f bridgeLogFunc) Log(ctx context.Context, l logging.Level, v ...any) { f(ctx, l, v...) }
