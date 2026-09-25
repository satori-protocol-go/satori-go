package server_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/go-chi/chi/v5"
	"github.com/gorilla/websocket"
	"github.com/satori-protocol-go/satori-go/pkg/satori/logging"
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
	"net/textproto"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

func makeServer(t *testing.T, cfg satoriserver.Config) *satoriserver.Server {
	t.Helper()
	srv, err := satoriserver.NewServer(cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := srv.Close(); err != nil {
			t.Error(err)
		}
	})
	return srv
}
func serverHandler(t *testing.T, srv *satoriserver.Server) http.Handler {
	t.Helper()
	h, err := srv.Handler()
	if err != nil {
		t.Fatal(err)
	}
	return h
}
func request(h http.Handler, method, path, body, token string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, path, strings.NewReader(body))
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("Authorization", token)
	protocol.SetIdentityHeaders(r.Header, "mock", "bot")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}

type serverLogFunc func(context.Context, logging.Level, ...any)

func (f serverLogFunc) Log(ctx context.Context, l logging.Level, v ...any) { f(ctx, l, v...) }

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

func TestServerHTTP(t *testing.T) {
	var logs []string
	logger := serverLogFunc(func(_ context.Context, l logging.Level, v ...any) {
		logs = append(logs, string(l)+" "+fmt.Sprint(v...))
	})
	srv := makeServer(t, satoriserver.Config{Token: "fixture", Logger: logger})
	info := &login.Login{Sn: 0, Platform: "mock", User: &user.User{Id: "bot"}, Status: login.LoginStatusOnline}
	if err := srv.Apply(&mockProvider{logins: []*login.Login{info}, proxyUrls: []string{"https://example.invalid/assets/"}}); err != nil {
		t.Fatal(err)
	}
	srv.Route(protocol.ApiMessageCreate, func(r *satoriserver.Request[any]) (any, error) { return r.Params, nil })
	srv.Route(protocol.ApiMessageList, satoriserver.Wrapper(func(r *satoriserver.Request[satoriserver.MessageListParam]) (any, error) {
		return map[string]int64{"value": r.Params.Limit.ValueOr(0)}, nil
	}))
	srv.Route(protocol.ApiGuildMemberMute, satoriserver.Wrapper(func(r *satoriserver.Request[satoriserver.GuildMemberMuteParam]) (any, error) {
		return map[string]int64{"value": r.Params.Duration}, nil
	}))
	srv.Route(protocol.ApiUserGet, func(*satoriserver.Request[any]) (any, error) {
		return nil, satoriserver.NewActionError(502, "upstream failure", nil)
	})
	h := serverHandler(t, srv)
	for _, tc := range []struct {
		name, path, method, token, body string
		status                          int
	}{
		{"message", "message.create", "POST", "Bearer fixture", `{"content":"你好","count":12345678901234567890}`, 200},
		{"empty-params", "message.create", "POST", "Bearer fixture", "", 200},
		{"malformed-json", "message.create", "POST", "Bearer fixture", `{"content":`, 400},
		{"extra-json", "message.create", "POST", "Bearer fixture", `{} {}`, 400},
		{"method", "message.create", "GET", "Bearer fixture", `{}`, 405},
		{"auth-missing", "message.create", "POST", "", `{}`, 401},
		{"auth-invalid", "meta", "POST", "Bearer other", `{}`, 401},
		{"meta", "meta", "POST", "Bearer fixture", `{}`, 200},
		{"upstream-error", "user.get", "POST", "Bearer fixture", `{"user_id":"u"}`, 502},
	} {
		t.Run(tc.name, func(t *testing.T) {
			w := request(h, tc.method, "/v1/"+tc.path, tc.body, tc.token)
			if w.Code != tc.status {
				t.Fatalf("%s: status=%d body=%s", tc.name, w.Code, w.Body)
			}
			switch tc.name {
			case "message":
				var data map[string]json.RawMessage
				if err := json.Unmarshal(w.Body.Bytes(), &data); err != nil {
					t.Fatal(err)
				}
				if string(data["content"]) != `"你好"` || string(data["count"]) != "12345678901234567890" {
					t.Fatalf("decoded=%s", w.Body)
				}
			case "empty-params":
				if w.Body.String() != "{}" {
					t.Fatalf("default params=%s", w.Body)
				}
			case "meta":
				if !strings.Contains(w.Body.String(), `"bot"`) || !strings.Contains(w.Body.String(), "https://example.invalid/assets/") {
					t.Fatalf("meta=%s", w.Body)
				}
			}
		})
	}
	for _, value := range []string{"0", "100", "9007199254740993", "9223372036854775807", `"10"`, "1.5"} {
		for _, tc := range []struct{ action, field string }{{"message.list", "limit"}, {"guild.member.mute", "duration"}} {
			w := request(h, "POST", "/v1/"+tc.action, `{"`+tc.field+`":`+value+`}`, "Bearer fixture")
			want := 200
			if value == `"10"` || value == "1.5" {
				want = 400
			}
			if w.Code != want || (want == 200 && w.Body.String() != `{"value":`+value+`}`) {
				t.Fatalf("%s=%s: %d %s", tc.field, value, w.Code, w.Body)
			}
		}
	}
	callback := make(chan []byte, 1)
	sink := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer reverse" || r.Header.Get(protocol.HeaderOpcode) != "5" {
			t.Errorf("webhook headers=%v", r.Header)
		}
		data, err := io.ReadAll(r.Body)
		if err != nil {
			t.Error(err)
		}
		callback <- data
		w.WriteHeader(200)
	}))
	defer sink.Close()
	raw, _ := json.Marshal(map[string]string{"url": sink.URL, "token": "reverse"})
	w := request(h, "POST", "/v1/meta/webhook.create", string(raw), "Bearer fixture")
	if w.Code != 200 {
		t.Fatalf("webhook create=%d %s", w.Code, w.Body)
	}
	select {
	case data := <-callback:
		if !bytes.Contains(data, []byte("https://example.invalid/assets/")) {
			t.Fatalf("webhook meta=%s", data)
		}
	case <-time.After(time.Second):
		t.Fatal("webhook delivery timeout")
	}
	if w := request(h, "POST", "/v1/meta/webhook.delete", string(raw), "Bearer fixture"); w.Code != 200 {
		t.Fatalf("webhook delete=%d", w.Code)
	}
	text := strings.Join(logs, "\n")
	for _, entry := range []string{"warn Satori HTTP authorization failed", "debug Satori RPC action=", "error Satori RPC action="} {
		if !strings.Contains(text, entry) {
			t.Fatalf("injected logs missing %q: %s", entry, text)
		}
	}
}

func TestServerRouting(t *testing.T) {
	for _, mode := range []string{"default", "external-config", "external-setter", "subpath"} {
		t.Run(mode, func(t *testing.T) {
			parent := chi.NewRouter()
			parent.Get("/custom", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(202) })
			cfg := satoriserver.Config{Host: "0.0.0.0", BaseHandler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(418)
				io.WriteString(w, r.Method+" "+r.URL.Path)
			})}
			if mode == "external-config" {
				cfg.ReplaceRouter = parent
			}
			srv := makeServer(t, cfg)
			if mode == "external-setter" {
				srv.ReplaceRouter(parent)
			}
			srv.RouteInternal("*", func(r *satoriserver.Request[satoriserver.InternalParam]) (any, error) {
				data, err := io.ReadAll(r.Origin.Body)
				return satoriserver.NewResponse(207, data), err
			})
			if err := srv.Handle("/healthz", http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(204) })); err != nil {
				t.Fatal(err)
			}
			if err := srv.Method("POST", "/only-post", http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(202) })); err != nil {
				t.Fatal(err)
			}
			var h http.Handler
			prefix := ""
			if mode == "subpath" {
				prefix = "/satori"
				parent.Route(prefix, func(r chi.Router) {
					if err := srv.RegisterRoutes(r); err != nil {
						t.Fatal(err)
					}
				})
				h = parent
			} else {
				h = serverHandler(t, srv)
			}
			for _, tc := range []struct {
				method, path, body string
				status             int
			}{{"POST", "/v1/meta", "{}", 200}, {"PATCH", "/v1/internal/echo", "native", 207}, {"GET", "/healthz", "", 204}, {"POST", "/only-post", "", 202}} {
				w := request(h, tc.method, prefix+tc.path, tc.body, "")
				if w.Code != tc.status || (tc.status == 207 && w.Body.String() != "native") {
					t.Fatalf("%s %s=%d %s", tc.method, tc.path, w.Code, w.Body)
				}
			}
			if mode != "subpath" {
				for _, path := range []string{"/outside", "/only-post"} {
					w := request(h, "GET", path, "", "")
					if w.Code != 418 || w.Body.String() != "GET "+path {
						t.Fatalf("fallback=%d %s", w.Code, w.Body)
					}
				}
			}
			if mode != "default" {
				if w := request(h, "GET", "/custom", "", ""); w.Code != 202 {
					t.Fatalf("custom=%d", w.Code)
				}
			}
		})
	}
}

func TestServerResources(t *testing.T) {
	srv := makeServer(t, satoriserver.Config{})
	if err := srv.Apply(&mockProvider{logins: []*login.Login{{Sn: 1, Platform: "mock", User: &user.User{Id: "other"}}}}); err != nil {
		t.Fatal(err)
	}
	directory := t.TempDir()
	path := filepath.Join(directory, "sample.txt")
	if err := os.WriteFile(path, []byte("static-data"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := srv.MountDir("/assets", directory, false); err != nil {
		t.Fatal(err)
	}
	if err := srv.MountFile("/single", path); err != nil {
		t.Fatal(err)
	}
	h := serverHandler(t, srv)
	for _, path := range []string{"/assets/sample.txt", "/single/sample.txt"} {
		w := request(h, "GET", path, "", "")
		if w.Code != 200 || w.Body.String() != "static-data" {
			t.Fatalf("static=%d %s", w.Code, w.Body)
		}
	}
	for _, named := range []bool{true, false} {
		var data bytes.Buffer
		form := multipart.NewWriter(&data)
		disposition := `form-data; name="resource"`
		if named {
			disposition += `; filename="sample.txt"`
		}
		part, err := form.CreatePart(textproto.MIMEHeader{"Content-Disposition": {disposition}, "Content-Type": {"text/plain"}})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := part.Write([]byte("resource-bytes")); err != nil {
			t.Fatal(err)
		}
		if err := form.Close(); err != nil {
			t.Fatal(err)
		}
		r := httptest.NewRequest("POST", "/v1/upload.create", &data)
		r.Header.Set("Content-Type", form.FormDataContentType())
		protocol.SetIdentityHeaders(r.Header, "mock", "bot")
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		var urls map[string]string
		if err := json.Unmarshal(w.Body.Bytes(), &urls); err != nil || w.Code != 200 || urls["resource"] == "" {
			t.Fatalf("upload=%d %s error=%v", w.Code, w.Body, err)
		}
		content, err := srv.GetLocalFile(urls["resource"])
		if err != nil || string(content) != "resource-bytes" {
			t.Fatalf("local=%q error=%v", content, err)
		}
		for _, tc := range []struct {
			target string
			status int
		}{{urls["resource"], 200}, {strings.Replace(urls["resource"], "/bot/", "/other/", 1), 403}, {"internal:mock/bot/_tmp/../outside", 400}} {
			result := request(h, "GET", "/v1/proxy/"+url.PathEscape(tc.target), "", "")
			if result.Code != tc.status || (tc.status == 200 && result.Body.String() != "resource-bytes") {
				t.Fatalf("proxy=%d %s", result.Code, result.Body)
			}
		}
	}
	small := makeServer(t, satoriserver.Config{MaxRequestBytes: 64})
	if err := small.Apply(&mockProvider{}); err != nil {
		t.Fatal(err)
	}
	var data bytes.Buffer
	form := multipart.NewWriter(&data)
	part, err := form.CreateFormFile("resource", "large.bin")
	if err != nil {
		t.Fatal(err)
	}
	part.Write(bytes.Repeat([]byte("x"), 128))
	form.Close()
	r := httptest.NewRequest("POST", "/v1/upload.create", &data)
	r.Header.Set("Content-Type", form.FormDataContentType())
	protocol.SetIdentityHeaders(r.Header, "mock", "bot")
	w := httptest.NewRecorder()
	serverHandler(t, small).ServeHTTP(w, r)
	if w.Code != 413 {
		t.Fatalf("upload limit=%d %s", w.Code, w.Body)
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

func TestServerDelivery(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := listener.Addr().(*net.TCPAddr).Port
	listener.Close()
	failedHook := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(503) }))
	defer failedHook.Close()
	delivered := make(chan struct{}, 8)
	healthyHook := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(200); delivered <- struct{}{} }))
	defer healthyHook.Close()
	srv, err := satoriserver.NewServer(satoriserver.Config{Port: port, Token: "fixture", Webhooks: []satoriserver.WebhookEndpoint{{URL: failedHook.URL}, {URL: healthyHook.URL}}})
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
		value := beta.Clone()
		if r.SelfID == "alpha" {
			value = alpha.Clone()
		}
		value.Adapter = "handler-result"
		return value, nil
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
	for id := range numbers {
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
		if err != nil || resp.StatusCode != 200 || info.User == nil || info.User.Id != id || info.Adapter != "handler-result" {
			t.Errorf("login.get handler result for %s: %+v status=%d err=%v", id, info, resp.StatusCode, err)
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
	resume := int64(0)
	replay, _ := connect(&resume)
	defer replay.Close()
	for i := 0; i < 3; i++ {
		select {
		case <-delivered:
		case <-time.After(time.Second):
			t.Fatal("healthy webhook delivery timed out")
		}
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

	if err := conn.WriteJSON(operation.Operation{Op: operation.OpcodePing}); err != nil {
		t.Fatal(err)
	}
	if err := conn.ReadJSON(&frame); err != nil || frame.Op != operation.OpcodePong {
		t.Fatalf("heartbeat=%d error=%v", frame.Op, err)
	}
}
