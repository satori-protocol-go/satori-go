package qq

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/WindowsSov8forUs/botgo-plus/dto"
	"github.com/WindowsSov8forUs/botgo-plus/interaction/signature"
	"github.com/go-chi/chi/v5"
	"github.com/gorilla/websocket"
	"github.com/satori-protocol-go/satori-go/pkg/satori/logging"
	"github.com/satori-protocol-go/satori-go/pkg/satori/model"
	"github.com/satori-protocol-go/satori-go/pkg/satori/model/event"
	"github.com/satori-protocol-go/satori-go/pkg/satori/model/login"
	"github.com/satori-protocol-go/satori-go/pkg/satori/model/message"
	"github.com/satori-protocol-go/satori-go/pkg/satori/server"
)

type qqRequest struct {
	Method, Path, Auth string
	Query              url.Values
	Fields             map[string]json.RawMessage
	Raw, Image         []byte
	Header             http.Header
}

type qqFixture struct {
	server  *httptest.Server
	adapter *Adapter
	mu      sync.Mutex
	calls   []qqRequest
	extra   func(http.ResponseWriter, *http.Request, qqRequest) bool
}

func (f *qqFixture) requests() []qqRequest {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]qqRequest(nil), f.calls...)
}

func newQQFixture(t *testing.T, configure func(*Config)) *qqFixture {
	t.Helper()
	f := &qqFixture{}
	f.server = httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, err := io.ReadAll(r.Body)
		if err != nil {
			t.Error(err)
			w.WriteHeader(400)
			return
		}
		item := qqRequest{Method: r.Method, Path: r.URL.Path, Auth: r.Header.Get("Authorization"), Query: r.URL.Query(), Header: r.Header.Clone(), Raw: raw, Fields: map[string]json.RawMessage{}}
		if strings.HasPrefix(r.Header.Get("Content-Type"), "multipart/form-data") {
			r.Body = io.NopCloser(bytes.NewReader(raw))
			if err := r.ParseMultipartForm(1 << 20); err != nil {
				t.Error(err)
				w.WriteHeader(400)
				return
			}
			defer r.MultipartForm.RemoveAll()
			for key, values := range r.MultipartForm.Value {
				value := values[0]
				if json.Valid([]byte(value)) {
					item.Fields[key] = json.RawMessage(value)
				} else {
					item.Fields[key], _ = json.Marshal(value)
				}
			}
			file, _, err := r.FormFile("file_image")
			if err != nil {
				t.Error(err)
				w.WriteHeader(400)
				return
			}
			item.Image, err = io.ReadAll(file)
			file.Close()
			if err != nil {
				t.Error(err)
				w.WriteHeader(400)
				return
			}
		} else if len(raw) > 0 && json.Valid(raw) {
			_ = json.Unmarshal(raw, &item.Fields)
		}
		f.mu.Lock()
		f.calls = append(f.calls, item)
		number := len(f.calls)
		extra := f.extra
		f.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("X-Tps-trace-ID", "fixture-trace")
		if extra != nil && extra(w, r, item) {
			return
		}
		if r.URL.Path == "/token" {
			w.Write([]byte(`{"access_token":"fixture","expires_in":7200}`))
			return
		}
		if strings.HasPrefix(r.URL.Path, "/object/") {
			if r.Method != "PUT" || r.Header.Get("Authorization") != "" {
				t.Errorf("object request=%s auth=%q", r.Method, r.Header.Get("Authorization"))
			}
			w.WriteHeader(204)
			return
		}
		if item.Auth != "QQBot fixture" {
			t.Errorf("QQ auth=%q path=%s", item.Auth, item.Path)
			w.WriteHeader(401)
			return
		}
		if r.URL.Path == "/users/@me" {
			json.NewEncoder(w).Encode(map[string]any{"id": "bot-" + r.Header.Get("X-Union-Appid"), "username": "fixture", "bot": true})
			return
		}
		if r.Method == "GET" && strings.HasSuffix(r.URL.Path, "/messages") {
			cursor := firstNonEmpty(r.URL.Query().Get("before"), r.URL.Query().Get("after"))
			if cursor == " opaque+/== " {
				w.Write([]byte(`[{"id":"new","seq_in_channel":"2"},{"id":"old","seq_in_channel":"1"}]`))
			} else {
				w.Write([]byte(`[]`))
			}
			return
		}
		if strings.HasSuffix(r.URL.Path, "/upload_prepare") {
			var prepare dto.UploadPrepareRequest
			if err := json.Unmarshal(raw, &prepare); err != nil {
				t.Error(err)
				w.WriteHeader(400)
				return
			}
			json.NewEncoder(w).Encode(dto.UploadPrepareResult{UploadID: "upload-id", BlockSize: prepare.FileSize, Parts: []dto.UploadPart{{Index: 0, BlockSize: prepare.FileSize, PresignedURL: f.server.URL + "/object/" + url.PathEscape(prepare.FileName)}}, UploadConfig: dto.UploadConfig{Concurrency: 1}})
			return
		}
		if strings.HasSuffix(r.URL.Path, "/upload_part_finish") {
			w.Write([]byte(`{}`))
			return
		}
		if strings.HasSuffix(r.URL.Path, "/files") {
			w.Write([]byte(`{"file_info":"opaque!file-info","file_uuid":"fixture-file","ttl":300}`))
			return
		}
		if r.Method == "POST" && strings.HasSuffix(r.URL.Path, "/messages") {
			var content string
			_ = json.Unmarshal(item.Fields["content"], &content)
			result := map[string]any{"id": fmt.Sprintf("sent-%d", number), "content": content, "ext_info": map[string]string{"ref_idx": "REFIDX_sent=="}}
			parts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
			if len(parts) == 4 && parts[1] == "groups" {
				result["group_openid"] = parts[2]
			}
			if len(parts) == 4 && parts[1] == "users" {
				result["author"] = map[string]string{"user_openid": parts[2]}
			}
			if len(parts) == 3 {
				result["channel_id"] = parts[1]
			}
			json.NewEncoder(w).Encode(result)
			return
		}
		if r.Method == "PUT" && strings.HasPrefix(r.URL.Path, "/interactions/") {
			w.WriteHeader(204)
			return
		}
		if r.Method == "DELETE" {
			w.WriteHeader(204)
			return
		}
		w.WriteHeader(404)
		w.Write([]byte(`{"code":11234,"message":"fixture endpoint not found"}`))
	}))
	cfg := Config{AppID: 123, Secret: "fixture-secret", TokenURL: f.server.URL + "/token", APIBaseURL: f.server.URL, HTTPClient: f.server.Client(), Logger: logging.NopLogger{}}
	if configure != nil {
		configure(&cfg)
	}
	adapter, err := New(cfg)
	if err != nil {
		f.server.Close()
		t.Fatal(err)
	}
	f.adapter = adapter
	t.Cleanup(func() { adapter.Cleanup(context.Background()); f.server.Close() })
	return f
}

func (f *qqFixture) call(action, platform, selfID string, params map[string]any) (any, error) {
	handler := f.adapter.Routes()[action]
	if handler == nil {
		return nil, fmt.Errorf("route %s missing", action)
	}
	return handler(&server.Request[any]{Action: action, Platform: platform, SelfID: selfID, Params: params})
}

func TestQQNativeRequests(t *testing.T) {
	f := newQQFixture(t, nil)
	logins, err := f.adapter.GetLogins(context.Background())
	if err != nil || len(logins) != 2 || logins[0].Sn == logins[1].Sn {
		t.Fatalf("logins=%+v error=%v", logins, err)
	}
	for _, tc := range []struct{ name, platform, target, content, path string }{
		{"group", "qq", "group", "hello", "/v2/groups/group/messages"},
		{"c2c", "qq", "private:user", "hello", "/v2/users/user/messages"},
		{"channel", "qqguild", "channel", "hello", "/channels/channel/messages"},
		{"channel-image", "qqguild", "channel", `<quote id="original"/><img src="data:image/png;base64,aW1hZ2U="/>`, "/channels/channel/messages"},
		{"group-image", "qq", "group", `<img src="https://example.invalid/image.png"/>`, "/v2/groups/group/messages"},
		{"c2c-audio", "qq", "private:user", `<audio src="https://example.invalid/audio.silk"/>`, "/v2/users/user/messages"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			result, err := f.call("message.create", tc.platform, "bot-123", map[string]any{"channel_id": tc.target, "content": tc.content, "referrer": map[string]any{"msg_id": "incoming", "msg_seq": 1}})
			if err != nil {
				t.Fatal(err)
			}
			messages := result.([]*message.Message)
			if len(messages) != 1 || messages[0].Id == "" {
				t.Fatalf("messages=%+v", messages)
			}
			calls := f.requests()
			last := calls[len(calls)-1]
			if last.Method != "POST" || last.Path != tc.path {
				t.Fatalf("request=%s %s", last.Method, last.Path)
			}
			if tc.name == "channel-image" {
				if string(last.Image) != "image" || !bytes.Contains(last.Fields["message_reference"], []byte("original")) {
					t.Fatalf("image=%q reference=%s", last.Image, last.Fields["message_reference"])
				}
			}
			if strings.Contains(tc.name, "group-image") || tc.name == "c2c-audio" {
				if !bytes.Contains(last.Fields["media"], []byte("opaque!file-info")) || string(last.Fields["msg_type"]) != "7" {
					t.Fatalf("media=%s", last.Raw)
				}
			}
		})
	}
}

func signedQQRequest(t *testing.T, raw []byte, appID, secret string) *http.Request {
	t.Helper()
	r := httptest.NewRequest("POST", "/qqbot", bytes.NewReader(raw))
	r.Header.Set("X-Bot-Appid", appID)
	r.Header.Set(signature.HeaderTimestamp, strconv.FormatInt(time.Now().Unix(), 10))
	signed, err := signature.Generate(secret, r.Header, raw)
	if err != nil {
		t.Fatal(err)
	}
	r.Header.Set("X-Signature-Ed25519", signed)
	return r
}

func TestQQWebhook(t *testing.T) {
	f := newQQFixture(t, nil)
	router := chi.NewRouter()
	f.adapter.RegisterRootRoutes(router)
	raw := []byte(`{"op":13,"d":{"plain_token":"plain","event_ts":"123"}}`)
	r := httptest.NewRequest("POST", "/qqbot", bytes.NewReader(raw))
	r.Header.Set("X-Bot-Appid", "123")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, r)
	var challenge struct {
		Plain     string `json:"plain_token"`
		Signature string `json:"signature"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &challenge); err != nil {
		t.Fatalf("challenge status=%d body=%s err=%v", w.Code, w.Body, err)
	}
	header := http.Header{}
	header.Set(signature.HeaderTimestamp, "123")
	expected, err := signature.Generate("fixture-secret", header, []byte("plain"))
	if err != nil || w.Code != 200 || challenge.Plain != "plain" || challenge.Signature != expected {
		t.Fatalf("challenge=%+v status=%d err=%v", challenge, w.Code, err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	stream := f.adapter.Publisher(ctx)
	raw = []byte(`{"op":0,"s":1,"t":"GROUP_AT_MESSAGE_CREATE","id":"event-id","d":{"id":"incoming","content":"hello","group_openid":"group","author":{"member_openid":"member"}}}`)
	w = httptest.NewRecorder()
	router.ServeHTTP(w, signedQQRequest(t, raw, "123", "fixture-secret"))
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"d":0`) {
		t.Fatalf("event ack=%d %s", w.Code, w.Body)
	}
	timer := time.NewTimer(2 * time.Second)
	defer timer.Stop()
	for {
		select {
		case evt := <-stream:
			if evt.Type == event.EventTypeMessageCreated {
				if evt.Login.Platform != "qq" || evt.Message.Id != "incoming" {
					t.Fatalf("event=%+v", evt)
				}
				return
			}
		case <-timer.C:
			t.Fatal("message event timed out")
		}
	}
}

func TestQQErrorAndNativeResponse(t *testing.T) {
	f := newQQFixture(t, nil)
	f.extra = func(w http.ResponseWriter, r *http.Request, item qqRequest) bool {
		if strings.Contains(r.URL.Path, "/denied/") {
			w.WriteHeader(403)
			w.Write([]byte(`{"err_code":11253}`))
			return true
		}
		if r.URL.Path == "/raw" {
			if r.Method != "PATCH" || r.URL.Query().Get("cursor") != " +/=" || r.Header.Get("Content-Type") != "application/octet-stream" {
				t.Errorf("native request=%s %s type=%s", r.Method, r.URL.RawQuery, r.Header.Get("Content-Type"))
			}
			w.Header().Set("Content-Type", "application/octet-stream")
			w.Write(item.Raw)
			return true
		}
		return false
	}
	for _, tc := range []struct {
		action, platform string
		params           map[string]any
		status           int
	}{
		{"channel.mute", "qq", map[string]any{"channel_id": "group"}, 404},
		{"guild.member.approve", "qq", map[string]any{"message_id": "request", "approve": true}, 501},
		{"message.create", "qq", map[string]any{"channel_id": "denied", "content": "hello"}, 403},
		{"message.create", "qqguild", map[string]any{"channel_id": "dm_user", "content": `<img src="data:image/png;base64,aW1hZ2U="/>`}, 501},
	} {
		_, err := f.call(tc.action, tc.platform, "bot-123", tc.params)
		var status interface{ HTTPStatus() int }
		if !errors.As(err, &status) || status.HTTPStatus() != tc.status {
			t.Errorf("%s status=%v wanted=%d", tc.action, err, tc.status)
		}
	}
	r := httptest.NewRequest("PATCH", "/v1/internal/raw?cursor=%20%2B%2F%3D", bytes.NewBufferString("binary-content"))
	r.Header.Set("Content-Type", "application/octet-stream")
	response, err := f.adapter.HandleInternal(server.Request[map[string]any]{Origin: r, Platform: "qq", SelfID: "bot-123"}, "_api/raw")
	if err != nil || response.StatusCode != 200 || string(response.Body) != "binary-content" || response.Header.Get("Content-Type") != "application/octet-stream" {
		t.Fatalf("native response=%+v error=%v", response, err)
	}
}

func TestQQMessagePagination(t *testing.T) {
	f := newQQFixture(t, nil)
	for _, direction := range []string{"before", "after"} {
		for _, order := range []string{"asc", "desc"} {
			params := map[string]any{"channel_id": "channel", "next": " opaque+/== ", "direction": direction, "order": order, "limit": 100}
			result, err := f.call("message.list", "qqguild", "bot-123", params)
			if err != nil {
				t.Fatal(err)
			}
			page := result.(*model.BidiPaginated[*message.Message])
			cursor := "old"
			if direction == "after" {
				cursor = "new"
			}
			first := "old"
			if order == "desc" {
				first = "new"
			}
			if len(page.Data) != 2 || page.Data[0].Id != first || page.Prev != cursor || page.Next != cursor {
				t.Fatalf("%s %s page=%+v", direction, order, page)
			}
			params["next"] = page.Next
			result, err = f.call("message.list", "qqguild", "bot-123", params)
			if err != nil {
				t.Fatal(err)
			}
			page = result.(*model.BidiPaginated[*message.Message])
			if len(page.Data) != 0 || page.Prev != "" || page.Next != "" {
				t.Fatalf("terminal page=%+v", page)
			}
		}
	}
}

func TestQQMultiAppGateway(t *testing.T) {
	var mu sync.Mutex
	connections := map[string]*websocket.Conn{}
	gateway := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := (&websocket.Upgrader{}).Upgrade(w, r, nil)
		if err != nil {
			t.Error(err)
			return
		}
		defer conn.Close()
		id := strings.TrimPrefix(r.URL.Path, "/")
		mu.Lock()
		connections[id] = conn
		mu.Unlock()
		if err := conn.WriteJSON(map[string]any{"op": 10, "d": map[string]int{"heartbeat_interval": 60000}}); err != nil {
			return
		}
		var identify dto.WSPayload
		if err := conn.ReadJSON(&identify); err != nil {
			t.Error(err)
			return
		}
		if identify.OPCode != dto.WSIdentity {
			t.Errorf("identify op=%d", identify.OPCode)
		}
		if err := conn.WriteJSON(map[string]any{"op": 0, "s": 1, "t": "READY", "d": map[string]any{"session_id": "session-" + id, "shard": []int{0, 1}, "user": map[string]any{"id": "bot-" + id}}}); err != nil {
			return
		}
		if err := conn.WriteJSON(map[string]any{"op": 0, "s": 2, "t": "GROUP_AT_MESSAGE_CREATE", "d": map[string]any{"id": "message-" + id, "content": "hello", "group_openid": "group-" + id, "author": map[string]string{"member_openid": "member"}}}); err != nil {
			return
		}
		for {
			if _, _, err := conn.ReadMessage(); err != nil {
				return
			}
		}
	}))
	defer gateway.Close()
	f := newQQFixture(t, func(cfg *Config) {
		cfg.Apps = []AppConfig{{AppID: 123, Secret: "fixture-secret"}, {AppID: 456, Secret: "second-secret"}}
		cfg.UseWebSocket = true
		cfg.WSReconnectDelay = time.Hour
	})
	f.extra = func(w http.ResponseWriter, r *http.Request, _ qqRequest) bool {
		if r.URL.Path != "/gateway/bot" {
			return false
		}
		id := r.Header.Get("X-Union-Appid")
		json.NewEncoder(w).Encode(dto.WebsocketAP{URL: "ws" + strings.TrimPrefix(gateway.URL, "http") + "/" + id, Shards: 1, SessionStartLimit: dto.SessionStartLimit{Total: 10, Remaining: 10, MaxConcurrency: 1}})
		return true
	}
	if err := f.adapter.Prepare(context.Background()); err != nil {
		t.Fatal(err)
	}
	initial, err := f.adapter.GetLogins(context.Background())
	if err != nil || len(initial) != 4 {
		t.Fatalf("initial logins=%+v error=%v", initial, err)
	}
	for _, info := range initial {
		if info.Status != login.LoginStatusConnect {
			t.Fatalf("initial state=%+v", info)
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- f.adapter.Block(ctx) }()
	received := map[string]bool{}
	timer := time.NewTimer(3 * time.Second)
	defer timer.Stop()
	for len(received) < 2 {
		select {
		case evt := <-f.adapter.Publisher(ctx):
			if evt.Type == event.EventTypeMessageCreated {
				id := strings.TrimPrefix(evt.Message.Id, "message-")
				if evt.Login.User.Id != "bot-"+id || evt.Login.Status != login.LoginStatusOnline {
					t.Fatalf("event ownership=%+v", evt)
				}
				received[id] = true
			}
		case <-timer.C:
			t.Fatal("multi-app READY/message timed out")
		}
	}
	mu.Lock()
	first := connections["123"]
	mu.Unlock()
	if err := first.WriteControl(websocket.CloseMessage, websocket.FormatCloseMessage(websocket.CloseGoingAway, "fixture close"), time.Now().Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(2 * time.Second)
	for {
		logins, err := f.adapter.GetLogins(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		ready := true
		for _, info := range logins {
			expected := login.LoginStatusOnline
			if info.User.Id == "bot-123" {
				expected = login.LoginStatusReconnect
			}
			ready = ready && info.Status == expected
		}
		if ready {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("per-app disconnect state did not converge")
		}
		time.Sleep(time.Millisecond)
	}
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("gateway shutdown timed out")
	}
}
