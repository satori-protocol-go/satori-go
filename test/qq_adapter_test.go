package testsuite

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	botgodto "github.com/WindowsSov8forUs/botgo-plus/dto"
	botgoopenapi "github.com/WindowsSov8forUs/botgo-plus/openapi"
	adapterqq "github.com/satori-protocol-go/satori-go/pkg/satori/adapter/qq"
	"github.com/satori-protocol-go/satori-go/pkg/satori/model/event"
	"github.com/satori-protocol-go/satori-go/pkg/satori/model/message"
	satoriserver "github.com/satori-protocol-go/satori-go/pkg/satori/server"
)

type qqMockOpenAPI struct {
	me *botgodto.User

	postMessageHook      func(channelID string, msg *botgodto.MessageToCreate)
	postMessageMultipart func(channelID string, msg *botgodto.MessageToCreate, fileImageData []byte)
	postDMMultipart      func(dm *botgodto.DirectMessage, msg *botgodto.MessageToCreate, fileImageData []byte)
	postGroupMessageHook func(groupID string, msg botgodto.APIMessage)
	postC2CMessageHook   func(userID string, msg botgodto.APIMessage)
}

func (m *qqMockOpenAPI) Me(ctx context.Context) (*botgodto.User, error) {
	_ = ctx
	if m.me != nil {
		return m.me, nil
	}
	return &botgodto.User{ID: "bot", Username: "bot", Bot: true}, nil
}

func (m *qqMockOpenAPI) Message(ctx context.Context, channelID string, messageID string) (*botgodto.Message, error) {
	_ = ctx
	return &botgodto.Message{ID: messageID, ChannelID: channelID, Content: "content"}, nil
}

func (m *qqMockOpenAPI) PostMessage(ctx context.Context, channelID string, msg *botgodto.MessageToCreate) (*botgodto.Message, error) {
	_ = ctx
	if m.postMessageHook != nil {
		m.postMessageHook(channelID, msg)
	}
	return &botgodto.Message{ID: "m1", ChannelID: channelID, Content: msg.Content}, nil
}

func (m *qqMockOpenAPI) PostMessageMultipart(
	ctx context.Context,
	channelID string,
	msg *botgodto.MessageToCreate,
	fileImageData []byte,
) (*botgodto.Message, error) {
	_ = ctx
	if m.postMessageMultipart != nil {
		m.postMessageMultipart(channelID, msg, fileImageData)
	}
	return &botgodto.Message{
		ID:        "mm1",
		ChannelID: channelID,
		Content:   msg.Content,
		Attachments: []*botgodto.MessageAttachment{
			{URL: "https://cdn.example/mm1.png", ContentType: "image/png"},
		},
	}, nil
}

func (m *qqMockOpenAPI) PostDirectMessage(
	ctx context.Context,
	dm *botgodto.DirectMessage,
	msg *botgodto.MessageToCreate,
) (*botgodto.Message, error) {
	_ = ctx
	return &botgodto.Message{
		ID:            "dm1",
		ChannelID:     dm.ChannelID,
		GuildID:       dm.GuildID,
		Content:       msg.Content,
		DirectMessage: true,
	}, nil
}

func (m *qqMockOpenAPI) PostDirectMessageMultipart(
	ctx context.Context,
	dm *botgodto.DirectMessage,
	msg *botgodto.MessageToCreate,
	fileImageData []byte,
) (*botgodto.Message, error) {
	_ = ctx
	if m.postDMMultipart != nil {
		m.postDMMultipart(dm, msg, fileImageData)
	}
	return &botgodto.Message{
		ID:            "dmm1",
		ChannelID:     dm.ChannelID,
		GuildID:       dm.GuildID,
		Content:       msg.Content,
		DirectMessage: true,
		Attachments: []*botgodto.MessageAttachment{
			{URL: "https://cdn.example/dmm1.png", ContentType: "image/png"},
		},
	}, nil
}

func (m *qqMockOpenAPI) RetractMessage(
	ctx context.Context,
	channelID string,
	msgID string,
	options ...botgoopenapi.RetractMessageOption,
) error {
	_ = ctx
	_ = channelID
	_ = msgID
	_ = options
	return nil
}

func (m *qqMockOpenAPI) RetractDMMessage(
	ctx context.Context,
	guildID string,
	msgID string,
	options ...botgoopenapi.RetractMessageOption,
) error {
	_ = ctx
	_ = guildID
	_ = msgID
	_ = options
	return nil
}

func (m *qqMockOpenAPI) CreateDirectMessage(
	ctx context.Context,
	dm *botgodto.DirectMessageToCreate,
) (*botgodto.DirectMessage, error) {
	_ = ctx
	_ = dm
	return &botgodto.DirectMessage{GuildID: "dmGuild", ChannelID: "dmChannel"}, nil
}

func (m *qqMockOpenAPI) PostGroupMessage(
	ctx context.Context,
	groupID string,
	msg botgodto.APIMessage,
) (*botgodto.GroupMessageResponse, error) {
	_ = ctx
	if m.postGroupMessageHook != nil {
		m.postGroupMessageHook(groupID, msg)
	}
	if media, ok := msg.(*botgodto.RichMediaMessage); ok {
		return &botgodto.GroupMessageResponse{
			MediaResponse: &botgodto.MediaResponse{
				FileInfo: fmt.Sprintf("group-file-info-%d", media.FileType),
			},
		}, nil
	}
	text := ""
	if typed, ok := msg.(*botgodto.MessageToCreate); ok {
		text = typed.Content
	}
	return &botgodto.GroupMessageResponse{
		Message: &botgodto.Message{ID: "gm1", GroupID: groupID, Content: text},
	}, nil
}

func (m *qqMockOpenAPI) PostC2CMessage(
	ctx context.Context,
	userID string,
	msg botgodto.APIMessage,
) (*botgodto.C2CMessageResponse, error) {
	_ = ctx
	if m.postC2CMessageHook != nil {
		m.postC2CMessageHook(userID, msg)
	}
	if media, ok := msg.(*botgodto.RichMediaMessage); ok {
		return &botgodto.C2CMessageResponse{
			MediaResponse: &botgodto.MediaResponse{
				FileInfo: fmt.Sprintf("c2c-file-info-%d", media.FileType),
			},
		}, nil
	}
	text := ""
	if typed, ok := msg.(*botgodto.MessageToCreate); ok {
		text = typed.Content
	}
	return &botgodto.C2CMessageResponse{
		Message: &botgodto.Message{ID: "cm1", Content: text, Author: &botgodto.User{UserOpenID: userID}},
	}, nil
}

func (m *qqMockOpenAPI) RetractGroupMessage(
	ctx context.Context,
	groupID string,
	msgID string,
	options ...botgoopenapi.RetractMessageOption,
) error {
	_ = ctx
	_ = groupID
	_ = msgID
	_ = options
	return nil
}

func (m *qqMockOpenAPI) RetractC2CMessage(
	ctx context.Context,
	userID string,
	msgID string,
	options ...botgoopenapi.RetractMessageOption,
) error {
	_ = ctx
	_ = userID
	_ = msgID
	_ = options
	return nil
}

func newQQTestAdapter(t *testing.T, mock *qqMockOpenAPI) *adapterqq.Adapter {
	t.Helper()
	adapter, err := adapterqq.New(adapterqq.Config{
		AppID:              123,
		Secret:             "secret",
		SkipTokenInit:      true,
		SkipSignatureCheck: true,
		APIV1:              mock,
		APIV2:              mock,
	})
	if err != nil {
		t.Fatalf("new adapter failed: %v", err)
	}
	return adapter
}

func TestQQAdapterGetLoginsAndEnsure(t *testing.T) {
	mock := &qqMockOpenAPI{me: &botgodto.User{ID: "bot-1", Username: "tester", Bot: true}}
	adapter := newQQTestAdapter(t, mock)

	logins, err := adapter.GetLogins(context.Background())
	if err != nil {
		t.Fatalf("get logins failed: %v", err)
	}
	if len(logins) != 2 {
		t.Fatalf("unexpected login count: %d", len(logins))
	}
	if !adapter.Ensure("qq", "bot-1") {
		t.Fatal("qq login ensure failed")
	}
	if !adapter.Ensure("qqguild", "bot-1") {
		t.Fatal("qqguild login ensure failed")
	}
}

func TestQQAdapterMessageCreate(t *testing.T) {
	mock := &qqMockOpenAPI{me: &botgodto.User{ID: "bot-2", Username: "tester", Bot: true}}
	adapter := newQQTestAdapter(t, mock)

	var groupCalls int
	var privateCalls int
	mock.postGroupMessageHook = func(groupID string, msg botgodto.APIMessage) {
		_ = groupID
		_ = msg
		groupCalls++
	}
	mock.postC2CMessageHook = func(userID string, msg botgodto.APIMessage) {
		_ = userID
		_ = msg
		privateCalls++
	}

	route, ok := adapter.Routes()[string(satoriserver.ApiMessageCreate)]
	if !ok {
		t.Fatal("message.create route not found")
	}

	_, err := route(satoriserver.Request[any]{
		Action:   string(satoriserver.ApiMessageCreate),
		Platform: "qq",
		SelfID:   "bot-2",
		Params: map[string]any{
			"channel_id": "group-1",
			"content":    "hello group",
		},
	})
	if err != nil {
		t.Fatalf("group message.create failed: %v", err)
	}

	_, err = route(satoriserver.Request[any]{
		Action:   string(satoriserver.ApiMessageCreate),
		Platform: "qq",
		SelfID:   "bot-2",
		Params: map[string]any{
			"channel_id": "private:user-1",
			"content":    "hello private",
		},
	})
	if err != nil {
		t.Fatalf("private message.create failed: %v", err)
	}

	if groupCalls != 1 {
		t.Fatalf("unexpected group calls: %d", groupCalls)
	}
	if privateCalls != 1 {
		t.Fatalf("unexpected private calls: %d", privateCalls)
	}
}

func TestQQAdapterWebhookValidationAndDispatch(t *testing.T) {
	mock := &qqMockOpenAPI{me: &botgodto.User{ID: "bot-3", Username: "tester", Bot: true}}
	adapter := newQQTestAdapter(t, mock)

	routes := adapter.RootRoutes()
	if len(routes) == 0 {
		t.Fatal("root routes not found")
	}
	handler := routes[0].Handler

	validationBody := map[string]any{
		"op": int(botgodto.HTTPCallbackValidation),
		"d": map[string]any{
			"plain_token": "plain",
			"event_ts":    "123",
		},
	}
	validationRaw, _ := json.Marshal(validationBody)
	validationReq := httptest.NewRequest(http.MethodPost, routes[0].Path, bytes.NewReader(validationRaw))
	validationReq.Header.Set("X-Bot-Appid", "123")
	validationResp := httptest.NewRecorder()
	handler.ServeHTTP(validationResp, validationReq)
	if validationResp.Code != http.StatusOK {
		t.Fatalf("validation status mismatch: %d", validationResp.Code)
	}

	var validationResult map[string]string
	if err := json.Unmarshal(validationResp.Body.Bytes(), &validationResult); err != nil {
		t.Fatalf("decode validation result failed: %v", err)
	}
	if validationResult["plain_token"] != "plain" {
		t.Fatalf("unexpected plain token: %#v", validationResult)
	}
	if validationResult["signature"] == "" {
		t.Fatalf("signature is empty: %#v", validationResult)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	stream := adapter.Publisher(ctx)

	dispatchBody := map[string]any{
		"op": 0,
		"t":  string(botgodto.EventGroupAtMessageCreate),
		"d": map[string]any{
			"id":       "m-1",
			"content":  "hello",
			"group_id": "group-1",
			"author": map[string]any{
				"member_openid": "user-1",
				"username":      "alice",
			},
		},
	}
	dispatchRaw, _ := json.Marshal(dispatchBody)
	dispatchReq := httptest.NewRequest(http.MethodPost, routes[0].Path, bytes.NewReader(dispatchRaw))
	dispatchReq.Header.Set("X-Bot-Appid", "123")
	dispatchResp := httptest.NewRecorder()
	handler.ServeHTTP(dispatchResp, dispatchReq)
	if dispatchResp.Code != http.StatusOK {
		t.Fatalf("dispatch status mismatch: %d", dispatchResp.Code)
	}

	deadline := time.After(2 * time.Second)
	for {
		select {
		case evt := <-stream:
			if evt != nil && evt.Type == event.EventTypeMessageCreated {
				if evt.Login == nil || evt.Login.Platform != "qq" {
					t.Fatalf("unexpected login platform: %#v", evt.Login)
				}
				if evt.Message == nil || evt.Message.Id != "m-1" {
					t.Fatalf("unexpected message payload: %#v", evt.Message)
				}
				return
			}
		case <-deadline:
			t.Fatal("timeout waiting message-created event")
		}
	}
}

func TestQQAdapterMessageCreateQQResourceSegments(t *testing.T) {
	mock := &qqMockOpenAPI{me: &botgodto.User{ID: "bot-4", Username: "tester", Bot: true}}
	adapter := newQQTestAdapter(t, mock)

	type sendRecord struct {
		kind string
		seq  int
	}
	records := make([]sendRecord, 0, 8)
	mock.postGroupMessageHook = func(groupID string, msg botgodto.APIMessage) {
		if groupID != "group-2" {
			t.Fatalf("unexpected group id: %s", groupID)
		}
		switch typed := msg.(type) {
		case *botgodto.RichMediaMessage:
			records = append(records, sendRecord{kind: fmt.Sprintf("upload-%d", typed.FileType)})
			if typed.SrvSendMsg {
				t.Fatalf("rich media upload should use srv_send_msg=false")
			}
		case *botgodto.MessageToCreate:
			records = append(records, sendRecord{kind: fmt.Sprintf("message-%d", typed.MsgType), seq: typed.MsgSeq})
		default:
			t.Fatalf("unexpected api message type: %T", msg)
		}
	}

	route, ok := adapter.Routes()[string(satoriserver.ApiMessageCreate)]
	if !ok {
		t.Fatal("message.create route not found")
	}

	result, err := route(satoriserver.Request[any]{
		Action:   string(satoriserver.ApiMessageCreate),
		Platform: "qq",
		SelfID:   "bot-4",
		Params: map[string]any{
			"channel_id": "group-2",
			"content":    `hello<img src="https://example.com/a.png"/><audio src="https://example.com/a.silk"/>`,
			"referrer": map[string]any{
				"msg_id":  "passive-msg",
				"msg_seq": -1,
			},
		},
	})
	if err != nil {
		t.Fatalf("message.create with resource failed: %v", err)
	}

	items, ok := result.([]*message.Message)
	if !ok {
		t.Fatalf("unexpected result type: %T", result)
	}
	if len(items) != 3 {
		t.Fatalf("unexpected message count: %d", len(items))
	}
	if len(records) != 5 {
		t.Fatalf("unexpected send call count: %d", len(records))
	}
	if records[0].kind != "message-0" || records[0].seq != 0 {
		t.Fatalf("unexpected first record: %+v", records[0])
	}
	if records[1].kind != "upload-1" {
		t.Fatalf("unexpected second record: %+v", records[1])
	}
	if records[2].kind != "message-7" || records[2].seq != 1 {
		t.Fatalf("unexpected third record: %+v", records[2])
	}
	if records[3].kind != "upload-3" {
		t.Fatalf("unexpected fourth record: %+v", records[3])
	}
	if records[4].kind != "message-7" || records[4].seq != 2 {
		t.Fatalf("unexpected fifth record: %+v", records[4])
	}
}

func TestQQAdapterMessageCreateQQGuildQuoteAndImage(t *testing.T) {
	mock := &qqMockOpenAPI{me: &botgodto.User{ID: "bot-5", Username: "tester", Bot: true}}
	adapter := newQQTestAdapter(t, mock)

	var called bool
	mock.postMessageHook = func(channelID string, msg *botgodto.MessageToCreate) {
		called = true
		if channelID != "guild-channel" {
			t.Fatalf("unexpected channel id: %s", channelID)
		}
		if msg.Image != "https://example.com/image.png" {
			t.Fatalf("unexpected image url: %s", msg.Image)
		}
		if msg.MessageReference == nil || msg.MessageReference.MessageID != "origin-msg" {
			t.Fatalf("unexpected message reference: %#v", msg.MessageReference)
		}
		if msg.MsgID != "passive-id" {
			t.Fatalf("unexpected passive msg_id: %s", msg.MsgID)
		}
	}

	route, ok := adapter.Routes()[string(satoriserver.ApiMessageCreate)]
	if !ok {
		t.Fatal("message.create route not found")
	}

	result, err := route(satoriserver.Request[any]{
		Action:   string(satoriserver.ApiMessageCreate),
		Platform: "qqguild",
		SelfID:   "bot-5",
		Params: map[string]any{
			"channel_id": "guild-channel",
			"content":    `<quote id="origin-msg"/><img src="https://example.com/image.png"/>`,
			"referrer": map[string]any{
				"msg_id": "passive-id",
			},
		},
	})
	if err != nil {
		t.Fatalf("message.create quote+image failed: %v", err)
	}
	if !called {
		t.Fatal("expected postMessageHook to be called")
	}
	items, ok := result.([]*message.Message)
	if !ok {
		t.Fatalf("unexpected result type: %T", result)
	}
	if len(items) != 1 {
		t.Fatalf("unexpected message count: %d", len(items))
	}
}

func TestQQAdapterMessageCreateQQGuildMultipartImage(t *testing.T) {
	mock := &qqMockOpenAPI{me: &botgodto.User{ID: "bot-6", Username: "tester", Bot: true}}
	adapter := newQQTestAdapter(t, mock)

	tempDir := t.TempDir()
	filePath := filepath.Join(tempDir, "image.png")
	if err := os.WriteFile(filePath, []byte("png-data"), 0o600); err != nil {
		t.Fatalf("write temp image failed: %v", err)
	}

	var multipartCalled bool
	mock.postMessageMultipart = func(channelID string, msg *botgodto.MessageToCreate, fileImageData []byte) {
		multipartCalled = true
		if channelID != "guild-channel-2" {
			t.Fatalf("unexpected channel id: %s", channelID)
		}
		if len(fileImageData) == 0 {
			t.Fatal("multipart image data is empty")
		}
		if msg.Image != "" {
			t.Fatalf("expected image url empty in multipart mode, got %s", msg.Image)
		}
	}

	route, ok := adapter.Routes()[string(satoriserver.ApiMessageCreate)]
	if !ok {
		t.Fatal("message.create route not found")
	}

	result, err := route(satoriserver.Request[any]{
		Action:   string(satoriserver.ApiMessageCreate),
		Platform: "qqguild",
		SelfID:   "bot-6",
		Params: map[string]any{
			"channel_id": "guild-channel-2",
			"content":    `<img src="file://` + filePath + `"/>`,
		},
	})
	if err != nil {
		t.Fatalf("message.create multipart image failed: %v", err)
	}
	if !multipartCalled {
		t.Fatal("expected multipart api to be called")
	}
	items, ok := result.([]*message.Message)
	if !ok {
		t.Fatalf("unexpected result type: %T", result)
	}
	if len(items) != 1 {
		t.Fatalf("unexpected message count: %d", len(items))
	}
}
