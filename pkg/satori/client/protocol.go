package client

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/textproto"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/satori-protocol-go/satori-go/pkg/satori/model/channel"
	"github.com/satori-protocol-go/satori-go/pkg/satori/model/define"
	"github.com/satori-protocol-go/satori-go/pkg/satori/model/event"
	"github.com/satori-protocol-go/satori-go/pkg/satori/model/guild"
	"github.com/satori-protocol-go/satori-go/pkg/satori/model/guildmember"
	"github.com/satori-protocol-go/satori-go/pkg/satori/model/guildrole"
	"github.com/satori-protocol-go/satori-go/pkg/satori/model/login"
	"github.com/satori-protocol-go/satori-go/pkg/satori/model/message"
	"github.com/satori-protocol-go/satori-go/pkg/satori/model/meta"
	"github.com/satori-protocol-go/satori-go/pkg/satori/model/user"
)

type APIProtocol struct {
	account *Account
	client  *http.Client
	timeout time.Duration
}

func NewAPIProtocol(account *Account, httpClient *http.Client) *APIProtocol {
	if account == nil {
		account = NewAccount(nil, APIInfo{}, nil, nil)
	}
	timeout := account.Config.TimeoutValue()
	if timeout <= 0 {
		timeout = 300 * time.Second
	}
	if httpClient == nil {
		httpClient = &http.Client{Timeout: timeout}
	} else if httpClient.Timeout == 0 {
		httpClient.Timeout = timeout
	}
	return &APIProtocol{
		account: account,
		client:  httpClient,
		timeout: timeout,
	}
}

func (p *APIProtocol) Download(ctx context.Context, rawURL string) ([]byte, error) {
	endpoint := p.account.EnsureURL(rawURL)
	return p.requestBytes(ctx, http.MethodGet, endpoint, nil, nil)
}

func (p *APIProtocol) RequestInternal(
	ctx context.Context,
	rawURL string,
	method string,
	params map[string]any,
) (map[string]any, error) {
	endpoint := p.account.EnsureURL(rawURL)
	method = normalizeMethod(method)

	var (
		body    io.Reader
		headers http.Header
		err     error
	)
	if method != http.MethodGet || len(params) > 0 {
		body, headers, err = encodeJSONBody(params)
		if err != nil {
			return nil, err
		}
	}
	payload, err := p.requestBytes(ctx, method, endpoint, body, headers)
	if err != nil {
		return nil, err
	}
	return decodeObject(payload)
}

func (p *APIProtocol) CallAPI(
	ctx context.Context,
	action string,
	params map[string]any,
	multipart bool,
	method string,
) (json.RawMessage, error) {
	action = strings.TrimSpace(action)
	if action == "" {
		return nil, errors.New("action cannot be empty")
	}

	endpoint := joinURLPath(p.account.Config.APIBase(), action)
	headers := p.apiHeaders()

	if multipart {
		uploads := make(map[string]Upload, len(params))
		for name, raw := range params {
			upload, ok := raw.(Upload)
			if !ok {
				return nil, fmt.Errorf("multipart param %q is not an Upload", name)
			}
			uploads[name] = upload
		}
		body, contentType, err := encodeMultipartBody(uploads)
		if err != nil {
			return nil, err
		}
		headers.Set("Content-Type", contentType)
		payload, err := p.requestBytes(ctx, http.MethodPost, endpoint, body, headers)
		if err != nil {
			return nil, err
		}
		return payload, nil
	}

	body, extraHeaders, err := encodeJSONBody(params)
	if err != nil {
		return nil, err
	}
	for key, values := range extraHeaders {
		for _, value := range values {
			headers.Add(key, value)
		}
	}

	payload, err := p.requestBytes(ctx, normalizeMethod(method), endpoint, body, headers)
	if err != nil {
		return nil, err
	}
	return payload, nil
}

func (p *APIProtocol) Send(ctx context.Context, evt *event.Event, messageText string) ([]*message.Message, error) {
	if evt == nil || evt.Channel == nil {
		return nil, errors.New("event cannot be replied to")
	}
	return p.SendMessage(ctx, evt.Channel.Id, messageText, evt.Referrer)
}

func (p *APIProtocol) SendMessage(
	ctx context.Context,
	channelID string,
	messageText string,
	referrer map[string]any,
) ([]*message.Message, error) {
	return p.MessageCreate(ctx, channelID, messageText, referrer)
}

func (p *APIProtocol) SendPrivateMessage(
	ctx context.Context,
	userID string,
	messageText string,
	referrer map[string]any,
) ([]*message.Message, error) {
	channelItem, err := p.UserChannelCreate(ctx, userID, "")
	if err != nil {
		return nil, err
	}
	return p.MessageCreate(ctx, channelItem.Id, messageText, referrer)
}

func (p *APIProtocol) UpdateMessage(ctx context.Context, channelID string, messageID string, content string) error {
	return p.MessageUpdate(ctx, channelID, messageID, content)
}

func (p *APIProtocol) MessageCreate(
	ctx context.Context,
	channelID string,
	content string,
	referrer map[string]any,
) ([]*message.Message, error) {
	resp, err := p.CallAPI(ctx, string(ApiMessageCreate), map[string]any{
		"channel_id": channelID,
		"content":    content,
		"referrer":   referrer,
	}, false, http.MethodPost)
	if err != nil {
		return nil, err
	}
	var result []*message.Message
	if err := decodeJSON(resp, &result); err != nil {
		return nil, err
	}
	return result, nil
}

func (p *APIProtocol) MessageGet(ctx context.Context, channelID string, messageID string) (*message.Message, error) {
	resp, err := p.CallAPI(ctx, string(ApiMessageGet), map[string]any{
		"channel_id": channelID,
		"message_id": messageID,
	}, false, http.MethodPost)
	if err != nil {
		return nil, err
	}
	var result message.Message
	if err := decodeJSON(resp, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

func (p *APIProtocol) MessageDelete(ctx context.Context, channelID string, messageID string) error {
	_, err := p.CallAPI(ctx, string(ApiMessageDelete), map[string]any{
		"channel_id": channelID,
		"message_id": messageID,
	}, false, http.MethodPost)
	return err
}

func (p *APIProtocol) MessageUpdate(ctx context.Context, channelID string, messageID string, content string) error {
	_, err := p.CallAPI(ctx, string(ApiMessageUpdate), map[string]any{
		"channel_id": channelID,
		"message_id": messageID,
		"content":    content,
	}, false, http.MethodPost)
	return err
}

func (p *APIProtocol) MessageList(
	ctx context.Context,
	channelID string,
	nextToken string,
	direction string,
	limit int,
	order string,
) (*define.BidiPaginated[*message.Message], error) {
	if direction == "" {
		direction = "before"
	}
	if limit <= 0 {
		limit = 50
	}
	if order == "" {
		order = "asc"
	}
	if nextToken == "" && direction != "before" {
		return nil, errors.New("invalid direction when next token is empty")
	}

	resp, err := p.CallAPI(ctx, string(ApiMessageList), map[string]any{
		"channel_id": channelID,
		"next":       nextToken,
		"direction":  direction,
		"limit":      limit,
		"order":      order,
	}, false, http.MethodPost)
	if err != nil {
		return nil, err
	}
	var result define.BidiPaginated[*message.Message]
	if err := decodeJSON(resp, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

func (p *APIProtocol) ChannelGet(ctx context.Context, channelID string) (*channel.Channel, error) {
	resp, err := p.CallAPI(ctx, string(ApiChannelGet), map[string]any{"channel_id": channelID}, false, http.MethodPost)
	if err != nil {
		return nil, err
	}
	var result channel.Channel
	if err := decodeJSON(resp, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

func (p *APIProtocol) ChannelList(
	ctx context.Context,
	guildID string,
	nextToken string,
) (*define.Paginated[*channel.Channel], error) {
	resp, err := p.CallAPI(ctx, string(ApiChannelList), map[string]any{
		"guild_id": guildID,
		"next":     nextToken,
	}, false, http.MethodPost)
	if err != nil {
		return nil, err
	}
	var result define.Paginated[*channel.Channel]
	if err := decodeJSON(resp, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

func (p *APIProtocol) ChannelCreate(ctx context.Context, guildID string, data *channel.Channel) (*channel.Channel, error) {
	resp, err := p.CallAPI(ctx, string(ApiChannelCreate), map[string]any{"guild_id": guildID, "data": data}, false, http.MethodPost)
	if err != nil {
		return nil, err
	}
	var result channel.Channel
	if err := decodeJSON(resp, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

func (p *APIProtocol) ChannelUpdate(ctx context.Context, channelID string, data *channel.Channel) error {
	_, err := p.CallAPI(ctx, string(ApiChannelUpdate), map[string]any{"channel_id": channelID, "data": data}, false, http.MethodPost)
	return err
}

func (p *APIProtocol) ChannelDelete(ctx context.Context, channelID string) error {
	_, err := p.CallAPI(ctx, string(ApiChannelDelete), map[string]any{"channel_id": channelID}, false, http.MethodPost)
	return err
}

func (p *APIProtocol) ChannelMute(ctx context.Context, channelID string, duration time.Duration) error {
	if duration <= 0 {
		duration = 60 * time.Second
	}
	_, err := p.CallAPI(ctx, string(ApiChannelMute), map[string]any{
		"channel_id": channelID,
		"duration":   duration.Milliseconds(),
	}, false, http.MethodPost)
	return err
}

func (p *APIProtocol) UserChannelCreate(ctx context.Context, userID string, guildID string) (*channel.Channel, error) {
	params := map[string]any{"user_id": userID}
	if guildID != "" {
		params["guild_id"] = guildID
	}
	resp, err := p.CallAPI(ctx, string(ApiUserChannelCreate), params, false, http.MethodPost)
	if err != nil {
		return nil, err
	}
	var result channel.Channel
	if err := decodeJSON(resp, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

func (p *APIProtocol) GuildGet(ctx context.Context, guildID string) (*guild.Guild, error) {
	resp, err := p.CallAPI(ctx, string(ApiGuildGet), map[string]any{"guild_id": guildID}, false, http.MethodPost)
	if err != nil {
		return nil, err
	}
	var result guild.Guild
	if err := decodeJSON(resp, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

func (p *APIProtocol) GuildList(ctx context.Context, nextToken string) (*define.Paginated[*guild.Guild], error) {
	resp, err := p.CallAPI(ctx, string(ApiGuildList), map[string]any{"next": nextToken}, false, http.MethodPost)
	if err != nil {
		return nil, err
	}
	var result define.Paginated[*guild.Guild]
	if err := decodeJSON(resp, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

func (p *APIProtocol) GuildApprove(ctx context.Context, requestID string, approve bool, comment string) error {
	_, err := p.CallAPI(ctx, string(ApiGuildApprove), map[string]any{
		"message_id": requestID,
		"approve":    approve,
		"comment":    comment,
	}, false, http.MethodPost)
	return err
}

func (p *APIProtocol) GuildMemberList(
	ctx context.Context,
	guildID string,
	nextToken string,
) (*define.Paginated[*guildmember.GuildMember], error) {
	resp, err := p.CallAPI(ctx, string(ApiGuildMemberList), map[string]any{
		"guild_id": guildID,
		"next":     nextToken,
	}, false, http.MethodPost)
	if err != nil {
		return nil, err
	}
	var result define.Paginated[*guildmember.GuildMember]
	if err := decodeJSON(resp, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

func (p *APIProtocol) GuildMemberGet(ctx context.Context, guildID string, userID string) (*guildmember.GuildMember, error) {
	resp, err := p.CallAPI(ctx, string(ApiGuildMemberGet), map[string]any{
		"guild_id": guildID,
		"user_id":  userID,
	}, false, http.MethodPost)
	if err != nil {
		return nil, err
	}
	var result guildmember.GuildMember
	if err := decodeJSON(resp, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

func (p *APIProtocol) GuildMemberKick(ctx context.Context, guildID string, userID string, permanent bool) error {
	_, err := p.CallAPI(ctx, string(ApiGuildMemberKick), map[string]any{
		"guild_id":  guildID,
		"user_id":   userID,
		"permanent": permanent,
	}, false, http.MethodPost)
	return err
}

func (p *APIProtocol) GuildMemberMute(
	ctx context.Context,
	guildID string,
	userID string,
	duration time.Duration,
) error {
	if duration <= 0 {
		duration = 60 * time.Second
	}
	_, err := p.CallAPI(ctx, string(ApiGuildMemberMute), map[string]any{
		"guild_id": guildID,
		"user_id":  userID,
		"duration": duration.Milliseconds(),
	}, false, http.MethodPost)
	return err
}

func (p *APIProtocol) GuildMemberApprove(ctx context.Context, requestID string, approve bool, comment string) error {
	_, err := p.CallAPI(ctx, string(ApiGuildMemberApprove), map[string]any{
		"message_id": requestID,
		"approve":    approve,
		"comment":    comment,
	}, false, http.MethodPost)
	return err
}

func (p *APIProtocol) GuildMemberRoleSet(ctx context.Context, guildID string, userID string, roleID string) error {
	_, err := p.CallAPI(ctx, string(ApiGuildMemberRoleSet), map[string]any{
		"guild_id": guildID,
		"user_id":  userID,
		"role_id":  roleID,
	}, false, http.MethodPost)
	return err
}

func (p *APIProtocol) GuildMemberRoleUnset(ctx context.Context, guildID string, userID string, roleID string) error {
	_, err := p.CallAPI(ctx, string(ApiGuildMemberRoleUnset), map[string]any{
		"guild_id": guildID,
		"user_id":  userID,
		"role_id":  roleID,
	}, false, http.MethodPost)
	return err
}

func (p *APIProtocol) GuildRoleList(
	ctx context.Context,
	guildID string,
	nextToken string,
) (*define.Paginated[*guildrole.GuildRole], error) {
	resp, err := p.CallAPI(ctx, string(ApiGuildRoleList), map[string]any{
		"guild_id": guildID,
		"next":     nextToken,
	}, false, http.MethodPost)
	if err != nil {
		return nil, err
	}
	var result define.Paginated[*guildrole.GuildRole]
	if err := decodeJSON(resp, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

func (p *APIProtocol) GuildRoleCreate(ctx context.Context, guildID string, role *guildrole.GuildRole) (*guildrole.GuildRole, error) {
	resp, err := p.CallAPI(ctx, string(ApiGuildRoleCreate), map[string]any{"guild_id": guildID, "role": role}, false, http.MethodPost)
	if err != nil {
		return nil, err
	}
	var result guildrole.GuildRole
	if err := decodeJSON(resp, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

func (p *APIProtocol) GuildRoleUpdate(ctx context.Context, guildID string, roleID string, role *guildrole.GuildRole) error {
	_, err := p.CallAPI(ctx, string(ApiGuildRoleUpdate), map[string]any{
		"guild_id": guildID,
		"role_id":  roleID,
		"role":     role,
	}, false, http.MethodPost)
	return err
}

func (p *APIProtocol) GuildRoleDelete(ctx context.Context, guildID string, roleID string) error {
	_, err := p.CallAPI(ctx, string(ApiGuildRoleDelete), map[string]any{
		"guild_id": guildID,
		"role_id":  roleID,
	}, false, http.MethodPost)
	return err
}

func (p *APIProtocol) ReactionCreate(ctx context.Context, channelID string, messageID string, emoji string) error {
	_, err := p.CallAPI(ctx, string(ApiReactionCreate), map[string]any{
		"channel_id": channelID,
		"message_id": messageID,
		"emoji":      emoji,
	}, false, http.MethodPost)
	return err
}

func (p *APIProtocol) ReactionDelete(
	ctx context.Context,
	channelID string,
	messageID string,
	emoji string,
	userID string,
) error {
	params := map[string]any{
		"channel_id": channelID,
		"message_id": messageID,
		"emoji":      emoji,
	}
	if userID != "" {
		params["user_id"] = userID
	}
	_, err := p.CallAPI(ctx, string(ApiReactionDelete), params, false, http.MethodPost)
	return err
}

func (p *APIProtocol) ReactionClear(ctx context.Context, channelID string, messageID string, emoji string) error {
	params := map[string]any{
		"channel_id": channelID,
		"message_id": messageID,
	}
	if emoji != "" {
		params["emoji"] = emoji
	}
	_, err := p.CallAPI(ctx, string(ApiReactionClear), params, false, http.MethodPost)
	return err
}

func (p *APIProtocol) ReactionList(
	ctx context.Context,
	channelID string,
	messageID string,
	emoji string,
	nextToken string,
) (*define.Paginated[*user.User], error) {
	resp, err := p.CallAPI(ctx, string(ApiReactionList), map[string]any{
		"channel_id": channelID,
		"message_id": messageID,
		"emoji":      emoji,
		"next":       nextToken,
	}, false, http.MethodPost)
	if err != nil {
		return nil, err
	}
	var result define.Paginated[*user.User]
	if err := decodeJSON(resp, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

func (p *APIProtocol) LoginGet(ctx context.Context) (*login.Login, error) {
	resp, err := p.CallAPI(ctx, string(ApiLoginGet), map[string]any{}, false, http.MethodPost)
	if err != nil {
		return nil, err
	}
	var result login.Login
	if err := decodeJSON(resp, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

func (p *APIProtocol) UserGet(ctx context.Context, userID string) (*user.User, error) {
	resp, err := p.CallAPI(ctx, string(ApiUserGet), map[string]any{"user_id": userID}, false, http.MethodPost)
	if err != nil {
		return nil, err
	}
	var result user.User
	if err := decodeJSON(resp, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

func (p *APIProtocol) FriendList(ctx context.Context, nextToken string) (*define.Paginated[*user.User], error) {
	resp, err := p.CallAPI(ctx, string(ApiFriendList), map[string]any{"next": nextToken}, false, http.MethodPost)
	if err != nil {
		return nil, err
	}
	var result define.Paginated[*user.User]
	if err := decodeJSON(resp, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

func (p *APIProtocol) FriendApprove(ctx context.Context, requestID string, approve bool, comment string) error {
	_, err := p.CallAPI(ctx, string(ApiFriendApprove), map[string]any{
		"message_id": requestID,
		"approve":    approve,
		"comment":    comment,
	}, false, http.MethodPost)
	return err
}

func (p *APIProtocol) Internal(ctx context.Context, action string, method string, params map[string]any) (any, error) {
	resp, err := p.CallAPI(ctx, "internal/"+strings.TrimPrefix(action, "/"), params, false, method)
	if err != nil {
		return nil, err
	}
	var result any
	if err := decodeJSON(resp, &result); err != nil {
		return nil, err
	}
	return result, nil
}

func (p *APIProtocol) MetaGet(ctx context.Context) (*meta.Meta, error) {
	resp, err := p.CallAPI(ctx, "meta", map[string]any{}, false, http.MethodPost)
	if err != nil {
		return nil, err
	}
	var result meta.Meta
	if err := decodeJSON(resp, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

func (p *APIProtocol) AdminLoginList(ctx context.Context) ([]*login.Login, error) {
	resp, err := p.CallAPI(ctx, "admin/login.list", map[string]any{}, false, http.MethodPost)
	if err != nil {
		return nil, err
	}
	var result []*login.Login
	if err := decodeJSON(resp, &result); err != nil {
		return nil, err
	}
	return result, nil
}

func (p *APIProtocol) WebhookCreate(ctx context.Context, endpoint string, token string) error {
	_, err := p.CallAPI(ctx, "meta/webhook.create", map[string]any{"url": endpoint, "token": token}, false, http.MethodPost)
	return err
}

func (p *APIProtocol) WebhookDelete(ctx context.Context, endpoint string) error {
	_, err := p.CallAPI(ctx, "meta/webhook.delete", map[string]any{"url": endpoint}, false, http.MethodPost)
	return err
}

func (p *APIProtocol) UploadCreateNamed(ctx context.Context, uploads map[string]Upload) (map[string]string, error) {
	params := make(map[string]any, len(uploads))
	for name, upload := range uploads {
		params[name] = upload
	}
	resp, err := p.CallAPI(ctx, string(ApiUploadCreate), params, true, http.MethodPost)
	if err != nil {
		return nil, err
	}
	var result map[string]string
	if err := decodeJSON(resp, &result); err != nil {
		return nil, err
	}
	return result, nil
}

func (p *APIProtocol) UploadCreate(ctx context.Context, uploads ...Upload) ([]string, error) {
	if len(uploads) == 0 {
		return []string{}, nil
	}
	params := make(map[string]Upload, len(uploads))
	for index, upload := range uploads {
		params[strconv.Itoa(index)] = upload
	}
	result, err := p.UploadCreateNamed(ctx, params)
	if err != nil {
		return nil, err
	}
	keys := make([]int, 0, len(result))
	for key := range result {
		parsed, parseErr := strconv.Atoi(key)
		if parseErr != nil {
			continue
		}
		keys = append(keys, parsed)
	}
	sort.Ints(keys)
	ordered := make([]string, 0, len(keys))
	for _, key := range keys {
		ordered = append(ordered, result[strconv.Itoa(key)])
	}
	return ordered, nil
}

func (p *APIProtocol) Upload(ctx context.Context, uploads ...Upload) ([]string, error) {
	return p.UploadCreate(ctx, uploads...)
}

func (p *APIProtocol) requestBytes(
	ctx context.Context,
	method string,
	endpoint string,
	body io.Reader,
	headers http.Header,
) ([]byte, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	request, err := http.NewRequestWithContext(ctx, method, endpoint, body)
	if err != nil {
		return nil, err
	}
	for key, values := range headers {
		for _, value := range values {
			request.Header.Add(key, value)
		}
	}
	response, err := p.client.Do(request)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()

	payload, err := io.ReadAll(response.Body)
	if err != nil {
		return nil, err
	}
	if response.StatusCode >= 200 && response.StatusCode < 300 {
		return payload, nil
	}
	return nil, &RequestError{StatusCode: response.StatusCode, Body: string(payload)}
}

func (p *APIProtocol) apiHeaders() http.Header {
	headers := http.Header{}
	headers.Set("Content-Type", "application/json")
	headers.Set("Authorization", "Bearer "+p.account.Config.TokenValue())
	headers.Set("X-Platform", p.account.Platform())
	headers.Set("X-Self-ID", p.account.SelfID())
	headers.Set("Satori-Platform", p.account.Platform())
	headers.Set("Satori-User-ID", p.account.SelfID())
	return headers
}

func encodeJSONBody(params map[string]any) (io.Reader, http.Header, error) {
	if params == nil {
		params = map[string]any{}
	}
	body, err := json.Marshal(params)
	if err != nil {
		return nil, nil, err
	}
	headers := http.Header{}
	headers.Set("Content-Type", "application/json")
	return bytes.NewReader(body), headers, nil
}

func encodeMultipartBody(uploads map[string]Upload) (io.Reader, string, error) {
	buffer := &bytes.Buffer{}
	writer := multipart.NewWriter(buffer)

	for name, rawUpload := range uploads {
		upload := rawUpload.normalized(name)
		header := textproto.MIMEHeader{}
		header.Set("Content-Disposition", fmt.Sprintf(`form-data; name=%q; filename=%q`, name, upload.Filename))
		header.Set("Content-Type", upload.ContentType)

		partWriter, err := writer.CreatePart(header)
		if err != nil {
			_ = writer.Close()
			return nil, "", err
		}
		if _, err := partWriter.Write(upload.Value); err != nil {
			_ = writer.Close()
			return nil, "", err
		}
	}

	if err := writer.Close(); err != nil {
		return nil, "", err
	}
	return buffer, writer.FormDataContentType(), nil
}

func decodeObject(payload []byte) (map[string]any, error) {
	if len(bytes.TrimSpace(payload)) == 0 {
		return map[string]any{}, nil
	}
	result := map[string]any{}
	if err := json.Unmarshal(payload, &result); err != nil {
		return nil, err
	}
	return result, nil
}

func decodeJSON(payload []byte, target any) error {
	if len(bytes.TrimSpace(payload)) == 0 {
		return nil
	}
	decoder := json.NewDecoder(bytes.NewReader(payload))
	decoder.UseNumber()
	return decoder.Decode(target)
}

func normalizeMethod(method string) string {
	method = strings.TrimSpace(strings.ToUpper(method))
	if method == "" {
		return http.MethodPost
	}
	return method
}
