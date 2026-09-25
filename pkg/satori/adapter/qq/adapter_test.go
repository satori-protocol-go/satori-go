package qq

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/WindowsSov8forUs/botgo-plus/interaction/signature"
	"github.com/satori-protocol-go/satori-go/pkg/satori/model/login"
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
	sdklog "github.com/WindowsSov8forUs/botgo-plus/log"
	"github.com/satori-protocol-go/satori-go/pkg/satori/logging"
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
	logs    *capturedQQLog
}

func (f *qqFixture) requests() []qqRequest {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]qqRequest(nil), f.calls...)
}

func newQQFixture(t *testing.T, configure func(*Config)) *qqFixture {
	t.Helper()
	f := &qqFixture{logs: &capturedQQLog{}}
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
			if r.Method != "PUT" {
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
		if r.Method == "GET" && strings.HasPrefix(r.URL.Path, "/channels/channel/messages/") {
			id := strings.TrimPrefix(r.URL.Path, "/channels/channel/messages/")
			value := map[string]any{"id": id, "channel_id": "channel", "content": "retrieved"}
			if id == "wrapped" {
				json.NewEncoder(w).Encode(map[string]any{"message": value})
			} else {
				json.NewEncoder(w).Encode(value)
			}
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
	cfg := Config{AppID: 123, Secret: "fixture-secret", TokenURL: f.server.URL + "/token", APIBaseURL: f.server.URL, HTTPClient: f.server.Client(), Logger: f.logs}
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

func TestQQWebhookDelivery(t *testing.T) {
	f := newQQFixture(t, func(cfg *Config) { cfg.EventBuffer = 1 })
	payload := func(id string) []byte {
		return []byte(fmt.Sprintf(`{"op":0,"s":1,"t":"GROUP_AT_MESSAGE_CREATE","d":{"id":%q,"content":"hello","group_openid":"g","author":{"member_openid":"u"}}}`, id))
	}
	first := httptest.NewRecorder()
	f.adapter.handleWebhookRequest(first, signedQQRequest(t, payload("first"), "123", "fixture-secret"))
	if first.Code != 200 {
		t.Fatalf("first response=%d %s", first.Code, first.Body.String())
	}
	done := make(chan *httptest.ResponseRecorder, 1)
	go func() {
		second := httptest.NewRecorder()
		f.adapter.handleWebhookRequest(second, signedQQRequest(t, payload("second"), "123", "fixture-secret"))
		done <- second
	}()
	select {
	case result := <-done:
		t.Fatalf("callback acknowledged before queue delivery: %d", result.Code)
	case <-time.After(30 * time.Millisecond):
	}
	firstEvent := <-f.adapter.eventCh
	if firstEvent.Message == nil || firstEvent.Message.Id != "first" {
		t.Fatalf("first event=%+v", firstEvent)
	}
	select {
	case result := <-done:
		if result.Code != 200 {
			t.Fatalf("second response=%d %s", result.Code, result.Body.String())
		}
	case <-time.After(time.Second):
		t.Fatal("callback did not finish after queue became available")
	}
	secondEvent := <-f.adapter.eventCh
	if secondEvent.Message == nil || secondEvent.Message.Id != "second" {
		t.Fatalf("second event=%+v", secondEvent)
	}
}

func TestQQNativeResponses(t *testing.T) {
	f := newQQFixture(t, nil)
	f.extra = func(w http.ResponseWriter, r *http.Request, item qqRequest) bool {
		if strings.Contains(r.URL.Path, "/denied/") {
			w.WriteHeader(403)
			w.Write([]byte(`{"err_code":11253}`))
			return true
		}
		if r.URL.Path == "/raw" {
			if r.Method != "PATCH" || len(r.URL.Query()["cursor"]) != 1 || r.URL.Query().Get("cursor") != " +/=" || r.Header.Get("Content-Type") != "application/octet-stream" || r.Header.Get("X-Native-Feature") != "original" {
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
	owner, err := server.NewServer(server.Config{Token: "fixture-token"})
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
	target := "/v1/proxy/" + url.PathEscape("internal:qq/bot-123/_api/raw") + "?cursor=%20%2B%2F%3D"
	req := httptest.NewRequest("PATCH", target, strings.NewReader("binary-content"))
	req.Header.Set("Content-Type", "application/octet-stream")
	req.Header.Set("Authorization", "Bearer fixture-token")
	req.Header.Set("X-Native-Feature", "original")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != 200 || rec.Body.String() != "binary-content" || rec.Header().Get("Content-Type") != "application/octet-stream" {
		t.Fatalf("proxied native response=%d %q", rec.Code, rec.Body.String())
	}
}

func TestQQMultiAppOwnershipAndShardStatus(t *testing.T) {
	f := newQQFixture(t, func(cfg *Config) {
		cfg.Apps = []AppConfig{{AppID: 123, Secret: "fixture-secret"}, {AppID: 456, Secret: "fixture-secret"}}
		cfg.UseWebSocket = true
	})
	ctx := context.Background()
	if err := f.adapter.Prepare(ctx); err != nil {
		t.Fatal(err)
	}
	if state := f.adapter.stateFromContextOrEvent(ctx, ""); state != nil {
		t.Fatalf("ambiguous event state=%+v", state)
	}
	if _, err := f.adapter.resolveStateBySelfID(ctx, ""); err == nil {
		t.Fatal("empty identity selected an App")
	}
	state, err := f.adapter.resolveStateBySelfID(ctx, "bot-456")
	if err != nil || state.appID != "456" {
		t.Fatalf("owner=%+v error=%v", state, err)
	}
	primary := f.adapter.appStates["123"]
	primary.expectedShards = 2
	check := func(want123, want456 login.LoginStatus) {
		t.Helper()
		items, err := f.adapter.GetLogins(ctx)
		if err != nil {
			t.Fatal(err)
		}
		for _, item := range items {
			want := want123
			if item.User.Id == "bot-456" {
				want = want456
			}
			if item.Status != want {
				t.Fatalf("login=%+v want status=%d", item, want)
			}
		}
	}
	check(login.LoginStatusConnect, login.LoginStatusConnect)
	if err := f.adapter.updateShardStatus(ctx, primary, 0, true); err != nil {
		t.Fatal(err)
	}
	check(login.LoginStatusReconnect, login.LoginStatusConnect)
	if err := f.adapter.updateShardStatus(ctx, primary, 1, true); err != nil {
		t.Fatal(err)
	}
	check(login.LoginStatusOnline, login.LoginStatusConnect)
	if err := f.adapter.updateShardStatus(ctx, primary, 0, false); err != nil {
		t.Fatal(err)
	}
	check(login.LoginStatusReconnect, login.LoginStatusConnect)
}

type capturedQQLog struct {
	mu       sync.Mutex
	levels   []logging.Level
	messages []string
	synced   bool
}

func (l *capturedQQLog) Log(_ context.Context, level logging.Level, values ...any) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.levels = append(l.levels, level)
	l.messages = append(l.messages, fmt.Sprint(values...))
}
func (l *capturedQQLog) Sync() error { l.synced = true; return nil }
func (l *capturedQQLog) text() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return strings.Join(l.messages, "\n")
}

func TestQQSDKLogging(t *testing.T) {
	captured := &capturedQQLog{}
	previous := sdklog.DefaultLogger
	RegisterSDKLogger(captured)
	defer func() { sdklog.DefaultLogger = previous }()
	sdklog.Debug("debug")
	sdklog.Info("info")
	sdklog.Warn("warn")
	sdklog.Error("error")
	sdklog.Debugf("%s", "debug")
	sdklog.Infof("%s", "info")
	sdklog.Warnf("%s", "warn")
	sdklog.Errorf("%s", "error")
	sdklog.Sync()
	want := []logging.Level{logging.LevelDebug, logging.LevelInfo, logging.LevelWarn, logging.LevelError}
	if len(captured.levels) != 8 || !captured.synced {
		t.Fatalf("SDK logger levels=%v synced=%t", captured.levels, captured.synced)
	}
	for i, level := range captured.levels {
		if level != want[i%4] {
			t.Fatalf("SDK level[%d]=%s", i, level)
		}
	}
}
