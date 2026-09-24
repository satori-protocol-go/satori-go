package network

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/satori-protocol-go/satori-go/pkg/satori/model/event"
	"github.com/satori-protocol-go/satori-go/pkg/satori/model/login"
	"github.com/satori-protocol-go/satori-go/pkg/satori/model/operation"
)

type networkApp struct {
	mu        sync.Mutex
	logins    []*login.Login
	proxies   []string
	events    []string
	fail      error
	delivered chan struct{}
}

func (a *networkApp) SyncLogins(_ string, _ APIConfig, proxies []string, logins []*login.Login) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.logins = logins
	a.proxies = proxies
	return nil
}
func (a *networkApp) UpdateProxyURLs(_ string, proxies []string) {
	a.mu.Lock()
	a.proxies = proxies
	a.mu.Unlock()
}
func (a *networkApp) PostEvent(_ string, evt *event.Event) error {
	// Make ordered dispatch observable even when the first handler is slower.
	if evt.Type == event.EventTypeLoginAdded {
		time.Sleep(10 * time.Millisecond)
	}
	a.mu.Lock()
	a.events = append(a.events, string(evt.Type))
	err := a.fail
	a.mu.Unlock()
	if a.delivered != nil {
		a.delivered <- struct{}{}
	}
	return err
}
func (a *networkApp) MarkNetworkStatus(string, login.LoginStatus, bool) {}

type networkConfig struct{ endpoint, token string }

func (c networkConfig) APIBase() string             { return c.endpoint }
func (c networkConfig) TokenValue() string          { return c.token }
func (c networkConfig) TimeoutValue() time.Duration { return 2 * time.Second }

func TestWebSocketConnection(t *testing.T) {
	frames := make(chan operation.Opcode, 4)
	gateway := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := (&websocket.Upgrader{}).Upgrade(w, r, nil)
		if err != nil {
			t.Error(err)
			return
		}
		defer conn.Close()
		var identify struct {
			Op   operation.Opcode       `json:"op"`
			Body operation.IdentifyBody `json:"body"`
		}
		if err := conn.ReadJSON(&identify); err != nil {
			t.Error(err)
			return
		}
		if identify.Op != operation.OpcodeIdentify || identify.Body.Token != "fixture" || identify.Body.Sn == nil || *identify.Body.Sn != 0 {
			t.Errorf("identify=%+v", identify)
		}
		for _, raw := range []string{
			`{"op":4,"body":{"logins":[],"proxy_urls":[]}}`,
			`{"op":0,"body":{"sn":1,"type":"login-added","timestamp":1,"login":{"sn":1,"platform":"p","user":{"id":"u"},"status":1,"adapter":"fixture"}}}`,
			`{"op":0,"body":{"sn":2,"type":"message-created","timestamp":2,"login":{"sn":1,"platform":"p","user":{"id":"u"}},"message":{"id":"m","content":"text"}}}`,
		} {
			if err := conn.WriteMessage(websocket.TextMessage, []byte(raw)); err != nil {
				t.Error(err)
				return
			}
		}
		pings := 0
		for {
			var frame struct {
				Op operation.Opcode `json:"op"`
			}
			if err := conn.ReadJSON(&frame); err != nil {
				return
			}
			frames <- frame.Op
			pings++
			if pings == 1 {
				if err := conn.WriteJSON(map[string]any{"op": operation.OpcodePong}); err != nil {
					return
				}
			}
		}
	}))
	defer gateway.Close()
	app := &networkApp{delivered: make(chan struct{}, 2)}
	ws := NewWS(app, WebSocketOptions{WSBase: "ws" + strings.TrimPrefix(gateway.URL, "http"), Token: "fixture", APIConfig: networkConfig{}})
	ws.base.SetSequence(0)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- ws.connectAndServe(ctx) }()
	defer ws.Close()
	if err := ws.WaitForAvailable(ctx); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		select {
		case <-app.delivered:
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		}
	}
	app.mu.Lock()
	ordered := strings.Join(app.events, ",")
	app.mu.Unlock()
	if ordered != "login-added,message-created" {
		t.Fatalf("events=%s", ordered)
	}
	if err := ws.heartbeatTick(); err != nil {
		t.Fatal(err)
	}
	select {
	case op := <-frames:
		if op != operation.OpcodePing {
			t.Fatalf("ping=%d", op)
		}
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	for ws.heartbeatPending.Load() {
		select {
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		case <-time.After(time.Millisecond):
		}
	}
	if err := ws.heartbeatTick(); err != nil {
		t.Fatal(err)
	}
	select {
	case <-frames:
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	if err := ws.heartbeatTick(); err == nil || err.Error() != "Satori PONG timeout" {
		t.Fatalf("heartbeat timeout=%v", err)
	}
	ws.Close()
	select {
	case <-done:
	case <-ctx.Done():
		t.Fatal("connection did not stop")
	}
	if ws.base.Sequence() != 2 {
		t.Fatalf("processed sequence=%d", ws.base.Sequence())
	}
}

func TestWebhookExchange(t *testing.T) {
	remote := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" || r.URL.Path != "/meta" || r.Header.Get("Authorization") != "Bearer api-token" {
			t.Errorf("meta request=%s %s auth=%q", r.Method, r.URL.Path, r.Header.Get("Authorization"))
			w.WriteHeader(401)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"logins":[{"sn":3,"platform":"p","user":{"id":"u"},"status":1,"adapter":"fixture"}],"proxy_urls":["https://old.example/"]}`))
	}))
	defer remote.Close()
	app := &networkApp{}
	hook := NewWebhook(app, WebhookOptions{Token: "callback-token", APIConfig: networkConfig{remote.URL, "api-token"}})
	if err := hook.fetchMeta(context.Background()); err != nil {
		t.Fatal(err)
	}
	evt := `{"sn":7,"type":"message-created","timestamp":1,"login":{"sn":3,"platform":"p","user":{"id":"u"}},"message":{"id":"m","content":"text"}}`
	for _, tc := range []struct {
		name, auth, op, body string
		failure              error
		status               int
	}{
		{"event", "Bearer callback-token", "0", evt, nil, 200},
		{"meta", "Bearer callback-token", "5", `{"proxy_urls":["https://new.example/"]}`, nil, 200},
		{"authentication", "Bearer wrong", "0", evt, nil, 401},
		{"callback-error", "Bearer callback-token", "0", evt, errors.New("fixture failed"), 503},
		{"opcode", "Bearer callback-token", "256", evt, nil, 400},
		{"malformed", "Bearer callback-token", "0", "{", nil, 400},
	} {
		t.Run(tc.name, func(t *testing.T) {
			app.fail = tc.failure
			r := httptest.NewRequest("POST", "/events", bytes.NewBufferString(tc.body))
			r.Header.Set("Authorization", tc.auth)
			r.Header.Set("Satori-Opcode", tc.op)
			w := httptest.NewRecorder()
			hook.handleRequest(w, r)
			if w.Code != tc.status {
				t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
			}
		})
	}
	app.fail = nil
	hook.token = ""
	r := httptest.NewRequest("POST", "/events", bytes.NewBufferString(evt))
	w := httptest.NewRecorder()
	hook.handleRequest(w, r)
	if w.Code != 200 || hook.base.Sequence() != 7 || len(app.logins) != 1 || len(app.proxies) != 1 || app.proxies[0] != "https://new.example/" {
		t.Fatalf("open callback=%d sequence=%d logins=%v proxies=%v", w.Code, hook.base.Sequence(), app.logins, app.proxies)
	}
}
