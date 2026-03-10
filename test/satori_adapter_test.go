package testsuite

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"strconv"
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

func (p *satoriRemoteProvider) HandleProxied(prefix string, rawURL string) (*satoriserver.Response, error) {
	_ = prefix
	_ = rawURL
	return nil, nil
}

func TestSatoriAdapterRouteForward(t *testing.T) {
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
	if err := remoteServer.Route(string(satoriserver.ApiMessageCreate), func(request satoriserver.Request[any]) (any, error) {
		params, ok := request.Params.(map[string]any)
		if !ok {
			return nil, satoriserver.BadRequest("invalid params")
		}
		content, _ := params["content"].(string)
		return []*message.Message{{Id: "1", Content: content}}, nil
	}); err != nil {
		t.Fatalf("register remote route failed: %v", err)
	}

	remoteCtx, remoteCancel := context.WithCancel(context.Background())
	defer remoteCancel()
	remoteErr := make(chan error, 1)
	go func() {
		remoteErr <- remoteServer.Run(remoteCtx)
	}()
	waitHTTPReady(t, fmt.Sprintf("http://127.0.0.1:%d/v1/meta", remotePort))

	adapter, err := satoriadapter.New(satoriadapter.Config{
		Host:  "127.0.0.1",
		Port:  remotePort,
		Token: "remote-secret",
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

	request, err := http.NewRequest(
		http.MethodPost,
		fmt.Sprintf("http://127.0.0.1:%d/v1/message.create", localPort),
		bytes.NewBufferString(`{"channel_id":"c1","content":"hello"}`),
	)
	if err != nil {
		t.Fatalf("new request failed: %v", err)
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("X-Platform", "satori")
	request.Header.Set("X-Self-ID", "bot")

	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatalf("call local server failed: %v", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(response.Body)
		t.Fatalf("status mismatch: got %d body=%s", response.StatusCode, string(body))
	}

	var payload []map[string]any
	if err := json.NewDecoder(response.Body).Decode(&payload); err != nil {
		t.Fatalf("decode response failed: %v", err)
	}
	if len(payload) != 1 || payload[0]["content"] != "hello" {
		t.Fatalf("unexpected payload: %#v", payload)
	}

	localCancel()
	remoteCancel()
	waitRunExit(t, localErr)
	waitRunExit(t, remoteErr)
}

func TestSatoriAdapterEventForward(t *testing.T) {
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

	remoteCtx, remoteCancel := context.WithCancel(context.Background())
	defer remoteCancel()
	remoteErr := make(chan error, 1)
	go func() {
		remoteErr <- remoteServer.Run(remoteCtx)
	}()
	waitHTTPReady(t, fmt.Sprintf("http://127.0.0.1:%d/v1/meta", remotePort))

	adapter, err := satoriadapter.New(satoriadapter.Config{
		Host:  "127.0.0.1",
		Port:  remotePort,
		Token: "remote-secret",
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

	wsURL := fmt.Sprintf("ws://127.0.0.1:%d/v1/events", localPort)
	connection, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("dial local websocket failed: %v", err)
	}
	defer connection.Close()

	if err := connection.WriteJSON(map[string]any{
		"op": operation.OpcodeIdentify,
		"body": map[string]any{
			"token":    "local-secret",
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

	if err := remoteServer.Post(&event.Event{
		Type:      event.EventTypeMessageCreated,
		Timestamp: time.Now().UnixMilli(),
		Login:     remoteLogin,
	}); err != nil {
		t.Fatalf("post remote event failed: %v", err)
	}

	_ = connection.SetReadDeadline(time.Now().Add(5 * time.Second))
	var pushed map[string]any
	if err := connection.ReadJSON(&pushed); err != nil {
		t.Fatalf("read pushed event failed: %v", err)
	}
	if toInt(t, pushed["op"]) != int(operation.OpcodeEvent) {
		t.Fatalf("unexpected opcode: %#v", pushed["op"])
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
	listener, err := netListen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen failed: %v", err)
	}
	defer listener.Close()
	_, portRaw, err := netSplitHostPort(listener.Addr().String())
	if err != nil {
		t.Fatalf("split host port failed: %v", err)
	}
	port, err := strconv.Atoi(portRaw)
	if err != nil {
		t.Fatalf("atoi port failed: %v", err)
	}
	return port
}

var (
	netListen        = net.Listen
	netSplitHostPort = net.SplitHostPort
)
