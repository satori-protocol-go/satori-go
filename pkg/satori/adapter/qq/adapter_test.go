package qq

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
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

func TestQQNativeEvents(t *testing.T) {
	f := newQQFixture(t, nil)
	cases := []struct {
		kind string
		data map[string]any
		want event.EventType
	}{
		{"GROUP_MESSAGE_CREATE", map[string]any{
			"id": "incoming", "group_openid": "group", "content": `hello <img src="file:///literal"/> <@member>`,
			"author":        map[string]any{"member_openid": "member", "member_role": "admin", "union_openid": "different-scope"},
			"message_scene": map[string]any{"ext": []string{"msg_idx=REFIDX_in==", "future=abc"}},
			"attachments":   []any{map[string]any{"content_type": "voice", "voice_wav_url": "https://example.invalid/voice.wav", "asr_refer_text": "speech"}},
			"ark_data":      map[string]any{"prompt": "card", "fields": map[string]any{"future": []int{1, 2}}},
			"msg_elements":  []any{map[string]any{"msg_idx": "REFIDX_child==", "content": "child", "msg_elements": []any{map[string]any{"content": "nested"}}}},
		}, event.EventTypeMessageCreated},
		{"GROUP_MEMBER_ADD", map[string]any{"group_openid": "group", "member_openid": "member", "member_role": "admin", "op_member_openid": "operator"}, event.EventTypeGuildMemberAdded},
		{"GROUP_JOIN_REQUEST", map[string]any{"group_openid": "group", "member_openid": "member", "join_request_id": "join-id"}, event.EventTypeGuildMemberRequest},
		{"FUTURE_QQ_EVENT", map[string]any{"opaque": json.Number("9007199254740993")}, event.EventTypeInternal},
	}
	for _, tc := range cases {
		t.Run(tc.kind, func(t *testing.T) {
			raw, err := json.Marshal(map[string]any{"op": 0, "s": 8, "t": tc.kind, "id": "native-event-id", "d": tc.data})
			if err != nil {
				t.Fatal(err)
			}
			response := httptest.NewRecorder()
			f.adapter.handleWebhookRequest(response, signedQQRequest(t, raw, "123", "fixture-secret"))
			if response.Code != 200 {
				t.Fatalf("callback=%d %s", response.Code, response.Body)
			}
			select {
			case evt := <-f.adapter.eventCh:
				if evt.Type != tc.want || evt.Login.Platform != "qq" || evt.Login.User.Id != "bot-123" || evt.Type_ != tc.kind {
					t.Fatalf("event=%+v", evt)
				}
				preserved, ok := evt.Data_.(json.RawMessage)
				if !ok || !bytes.Equal(raw, preserved) || evt.Referrer["qq_event_id"] != "native-event-id" {
					t.Fatalf("native context=%+v", evt.Referrer)
				}
				if tc.kind == "GROUP_MESSAGE_CREATE" {
					if evt.Channel.Id != "group" || evt.User.Id != "member" || len(evt.Member.Roles) != 1 || evt.Member.Roles[0].Id != "admin" || evt.Referrer["ref_idx"] != "REFIDX_in==" {
						t.Fatalf("group resources=%+v", evt)
					}
					for _, fragment := range []string{"&lt;img", `<at id="member"/>`, `<audio`, "voice.wav", "qq:ark-data", "nested", "REFIDX_child=="} {
						if !strings.Contains(evt.Message.Content, fragment) {
							t.Errorf("missing %q in %s", fragment, evt.Message.Content)
						}
					}
				}
				if tc.kind == "GROUP_JOIN_REQUEST" && evt.Message.Id != "join-id" {
					t.Fatalf("request message=%+v", evt.Message)
				}
			case <-time.After(time.Second):
				t.Fatal("converted event timed out")
			}
		})
	}
}

func TestQQReplyContext(t *testing.T) {
	f := newQQFixture(t, nil)
	result, err := f.call("message.create", "qq", "bot-123", map[string]any{
		"channel_id": "group", "content": `<quote id="REFIDX_quoted=="/>reply<message>next</message>`,
		"referrer": map[string]any{"msg_id": "incoming", "event_id": "reply-event", "msg_seq": 2, "app_id": "123"},
	})
	if err != nil {
		t.Fatal(err)
	}
	sent := result.([]*message.Message)
	if len(sent) != 2 || sent[1].Referrer["msg_seq"] != 4 || sent[1].Referrer["ref_idx"] != "REFIDX_sent==" {
		t.Fatalf("reply results=%+v", sent)
	}
	var calls []qqRequest
	for _, item := range f.requests() {
		if item.Path == "/v2/groups/group/messages" {
			calls = append(calls, item)
		}
	}
	for i, item := range calls {
		if string(item.Fields["msg_id"]) != `"incoming"` || string(item.Fields["event_id"]) != `"reply-event"` || string(item.Fields["msg_seq"]) != strconv.Itoa(3+i) {
			t.Fatalf("independent reply fields=%s", item.Raw)
		}
	}
	if !bytes.Contains(calls[0].Fields["message_reference"], []byte("REFIDX_quoted==")) {
		t.Fatalf("quote=%s", calls[0].Raw)
	}
	result, err = f.call("message.create", "qq", "bot-123", map[string]any{"channel_id": "group", "content": "continued", "referrer": sent[1].Referrer})
	if err != nil {
		t.Fatal(err)
	}
	if result.([]*message.Message)[0].Referrer["msg_seq"] != 5 {
		t.Fatalf("continued context=%+v", result)
	}
	content := strings.Repeat("<message>piece</message>", 7)
	result, err = f.call("message.create", "qq", "bot-123", map[string]any{"channel_id": "group", "content": content, "referrer": map[string]any{"msg_id": "incoming", "msg_seq": 5}})
	if err != nil || len(result.([]*message.Message)) != 7 {
		t.Fatalf("segmented reply=%+v err=%v", result, err)
	}
	result, err = f.call("message.create", "qq", "bot-123", map[string]any{"channel_id": "group", "content": `<qq:passive id="incoming" seq="7"/>passive`})
	if err != nil || result.([]*message.Message)[0].Referrer["msg_seq"] != 8 {
		t.Fatalf("passive context=%+v err=%v", result, err)
	}
	for _, input := range []map[string]any{
		{"msg_id": "conflict", "msg_seq": 7}, {"msg_id": "incoming", "msg_seq": 6}, {"msg_seq": json.Number("1.5")},
	} {
		_, err = f.call("message.create", "qq", "bot-123", map[string]any{"channel_id": "group", "content": `<qq:passive id="incoming" seq="7"/>text`, "referrer": input})
		var status interface{ HTTPStatus() int }
		if !errors.As(err, &status) || status.HTTPStatus() != 400 {
			t.Fatalf("conflicting context result=%v", err)
		}
	}
	f.mu.Lock()
	f.extra = func(w http.ResponseWriter, r *http.Request, item qqRequest) bool {
		if r.URL.Path == "/v2/groups/partial/messages" && string(item.Fields["content"]) == `"second"` {
			w.WriteHeader(403)
			w.Write([]byte(`{"err_code":11253}`))
			return true
		}
		return false
	}
	f.mu.Unlock()
	result, err = f.call("message.create", "qq", "bot-123", map[string]any{"channel_id": "partial", "content": "<message>first</message><message>second</message>"})
	if err != nil {
		t.Fatal(err)
	}
	response := result.(*server.Response)
	var partial struct {
		Error    string             `json:"error"`
		Messages []*message.Message `json:"messages"`
	}
	if err := json.Unmarshal(response.Body, &partial); err != nil {
		t.Fatal(err)
	}
	if response.StatusCode != 403 || len(partial.Messages) != 1 || partial.Messages[0].Id == "" || partial.Error == "" {
		t.Fatalf("partial result=%d %s", response.StatusCode, response.Body)
	}
}

func TestQQOwnedMediaPipeline(t *testing.T) {
	f := newQQFixture(t, nil)
	owner, err := server.NewServer(server.Config{Token: "satori-token"})
	if err != nil {
		t.Fatal(err)
	}
	defer owner.Close()
	if err := owner.Apply(f.adapter); err != nil {
		t.Fatal(err)
	}
	handler, err := owner.Handler()
	if err != nil {
		t.Fatal(err)
	}
	image := []byte("\x89PNG\r\n\x1a\nimage-body")
	for _, tc := range []struct{ platform, target, prepare string }{
		{"qq", "group", "/v2/groups/group/upload_prepare"},
		{"qq", "private:user", "/v2/users/user/upload_prepare"},
		{"qqguild", "channel", ""},
	} {
		t.Run(tc.target, func(t *testing.T) {
			body := bytes.NewBuffer(nil)
			form := multipart.NewWriter(body)
			part, err := form.CreateFormFile("image", "fixture.png")
			if err != nil {
				t.Fatal(err)
			}
			part.Write(image)
			form.Close()
			r := httptest.NewRequest("POST", "/v1/upload.create", body)
			r.Header.Set("Content-Type", form.FormDataContentType())
			r.Header.Set("Authorization", "Bearer satori-token")
			r.Header.Set("Satori-Platform", tc.platform)
			r.Header.Set("Satori-User-ID", "bot-123")
			uploaded := httptest.NewRecorder()
			handler.ServeHTTP(uploaded, r)
			var urls map[string]string
			if err := json.Unmarshal(uploaded.Body.Bytes(), &urls); err != nil || uploaded.Code != 200 {
				t.Fatalf("upload=%d %s err=%v", uploaded.Code, uploaded.Body, err)
			}
			before := len(f.requests())
			data, _ := json.Marshal(map[string]any{"channel_id": tc.target, "content": `<img title="fixture.png" src="` + urls["image"] + `"/>`})
			r = httptest.NewRequest("POST", "/v1/message.create", bytes.NewReader(data))
			r.Header.Set("Content-Type", "application/json")
			r.Header.Set("Authorization", "Bearer satori-token")
			r.Header.Set("Satori-Platform", tc.platform)
			r.Header.Set("Satori-User-ID", "bot-123")
			sent := httptest.NewRecorder()
			handler.ServeHTTP(sent, r)
			var messages []message.Message
			if err := json.Unmarshal(sent.Body.Bytes(), &messages); err != nil || sent.Code != 200 || len(messages) != 1 {
				t.Fatalf("send=%d %s err=%v", sent.Code, sent.Body, err)
			}
			calls := f.requests()[before:]
			if tc.prepare != "" {
				if len(calls) != 5 || calls[0].Path != tc.prepare || calls[1].Method != "PUT" || !bytes.Equal(calls[1].Raw, image) || string(calls[0].Fields["file_name"]) != `"fixture.png"` {
					t.Fatalf("media pipeline=%+v", calls)
				}
				if !strings.HasSuffix(calls[2].Path, "/upload_part_finish") || !strings.HasSuffix(calls[3].Path, "/files") || !bytes.Contains(calls[4].Fields["media"], []byte("opaque!file-info")) {
					t.Fatalf("media confirmation/message=%+v", calls)
				}
			} else if len(calls) != 1 || !bytes.Equal(calls[0].Image, image) {
				t.Fatalf("channel image=%+v", calls)
			}
		})
	}
	for _, src := range []string{"file:///not-an-rpc-resource", "data:image/png;base64,broken!"} {
		_, err := f.call("message.create", "qqguild", "bot-123", map[string]any{"channel_id": "channel", "content": `<img src="` + src + `"/>`})
		var status interface{ HTTPStatus() int }
		if !errors.As(err, &status) || status.HTTPStatus() != 400 {
			t.Fatalf("resource error=%v", err)
		}
	}
	f.mu.Lock()
	f.extra = func(w http.ResponseWriter, r *http.Request, _ qqRequest) bool {
		if r.URL.Path == "/v2/groups/failure/upload_prepare" {
			w.WriteHeader(403)
			w.Write([]byte(`{"err_code":11253}`))
			return true
		}
		return false
	}
	f.mu.Unlock()
	_, err = f.call("message.create", "qq", "bot-123", map[string]any{"channel_id": "failure", "content": `<img src="data:image/png;base64,aW1hZ2U="/>`})
	var status interface{ HTTPStatus() int }
	if !errors.As(err, &status) || status.HTTPStatus() != 403 || !strings.Contains(err.Error(), "prepare") {
		t.Fatalf("upload stage failure=%v", err)
	}
}

func TestQQAuditCorrelation(t *testing.T) {
	f := newQQFixture(t, nil)
	a := f.adapter
	a.captureAuditResult("123", "MESSAGE_AUDIT_PASS", []byte(`{"audit_id":"same","message_id":"first"}`))
	a.captureAuditResult("456", "MESSAGE_AUDIT_PASS", []byte(`{"audit_id":"same","message_id":"other-app"}`))
	a.captureAuditResult("123", "MESSAGE_AUDIT_PASS", []byte(`{"audit_id":"same","message_id":"first"}`))
	for _, tc := range []struct{ app, want string }{{"123", "first"}, {"456", "other-app"}} {
		result, err := a.waitAuditResult(context.Background(), tc.app, "same", time.Second)
		if err != nil || !result.Passed || result.MessageID != tc.want {
			t.Fatalf("early audit=%+v err=%v", result, err)
		}
	}
	type outcome struct {
		result auditResult
		err    error
	}
	completed := make(chan outcome, 1)
	go func() {
		result, err := a.waitAuditResult(context.Background(), "123", "late", time.Second)
		completed <- outcome{result, err}
	}()
	deadline := time.Now().Add(time.Second)
	for {
		a.auditMu.Lock()
		entry := a.audits[auditKey{"123", "late"}]
		waiting := entry != nil && entry.waiters == 1
		a.auditMu.Unlock()
		if waiting {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("audit waiter did not start")
		}
		time.Sleep(time.Millisecond)
	}
	a.captureAuditResult("123", "MESSAGE_AUDIT_PASS", []byte(`{"audit_id":"late","message_id":"late-approved"}`))
	result := <-completed
	if result.err != nil || result.result.MessageID != "late-approved" {
		t.Fatalf("late audit=%+v", result)
	}
	a.captureAuditResult("123", "MESSAGE_AUDIT_REJECT", []byte(`{"audit_id":"reject","reject_reason":"fixture policy"}`))
	rejected, err := a.waitAuditResult(context.Background(), "123", "reject", time.Second)
	if err != nil || rejected.Passed || rejected.Reason != "fixture policy" {
		t.Fatalf("rejected=%+v err=%v", rejected, err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := a.waitAuditResult(ctx, "123", "cancelled", time.Second); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled result=%v", err)
	}
	if _, err := a.waitAuditResult(context.Background(), "123", "unresolved", time.Millisecond); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("pending timeout=%v", err)
	}
	f.mu.Lock()
	f.extra = func(w http.ResponseWriter, r *http.Request, item qqRequest) bool {
		if r.URL.Path != "/v2/groups/audit/messages" {
			return false
		}
		kind := "MESSAGE_AUDIT_PASS"
		id := "approved"
		if string(item.Fields["content"]) == `"rejected"` {
			kind = "MESSAGE_AUDIT_REJECT"
			id = "rejected"
		}
		data, _ := json.Marshal(map[string]string{"audit_id": id, "message_id": id, "reject_reason": "fixture"})
		a.captureAuditResult("123", kind, data)
		json.NewEncoder(w).Encode(map[string]any{"err_code": 304024, "data": map[string]any{"message_audit": map[string]string{"audit_id": id}}})
		return true
	}
	f.mu.Unlock()
	sent, err := f.call("message.create", "qq", "bot-123", map[string]any{"channel_id": "audit", "content": "approved"})
	if err != nil || sent.([]*message.Message)[0].Id != "approved" {
		t.Fatalf("audited message=%+v err=%v", sent, err)
	}
	_, err = f.call("message.create", "qq", "bot-123", map[string]any{"channel_id": "audit", "content": "rejected"})
	var status interface{ HTTPStatus() int }
	if !errors.As(err, &status) || status.HTTPStatus() != 403 || !strings.Contains(err.Error(), "audit rejected") {
		t.Fatalf("audit rejection response=%v", err)
	}
}
