package client

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/textproto"
	"net/url"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/satori-protocol-go/satori-go/pkg/satori/model"
	"github.com/satori-protocol-go/satori-go/pkg/satori/model/channel"
	"github.com/satori-protocol-go/satori-go/pkg/satori/model/event"
	"github.com/satori-protocol-go/satori-go/pkg/satori/model/guild"
	"github.com/satori-protocol-go/satori-go/pkg/satori/model/guildmember"
	"github.com/satori-protocol-go/satori-go/pkg/satori/model/guildrole"
	"github.com/satori-protocol-go/satori-go/pkg/satori/model/login"
	"github.com/satori-protocol-go/satori-go/pkg/satori/model/message"
	"github.com/satori-protocol-go/satori-go/pkg/satori/model/message/element"
	"github.com/satori-protocol-go/satori-go/pkg/satori/model/meta"
	"github.com/satori-protocol-go/satori-go/pkg/satori/model/paginated"
	"github.com/satori-protocol-go/satori-go/pkg/satori/model/user"
	"github.com/satori-protocol-go/satori-go/pkg/satori/protocol"
)

type APIProtocol struct {
	account *Account
	client  *http.Client
	timeout time.Duration
}

type InternalRequestOptions struct {
	params      map[string]any
	headers     http.Header
	query       url.Values
	cookies     []*http.Cookie
	body        io.Reader
	contentType string
	timeout     time.Duration
	timeoutSet  bool
	proxy       string
	proxySet    bool
	basicUser   string
	basicPass   string
	basicSet    bool
	bearerToken string
	tlsConfig   *tls.Config
}

type RequestOption func(*InternalRequestOptions)

func WithRequestHeader(key string, value string) RequestOption {
	return func(options *InternalRequestOptions) {
		if options == nil || strings.TrimSpace(key) == "" {
			return
		}
		if options.headers == nil {
			options.headers = http.Header{}
		}
		options.headers.Add(key, value)
	}
}

func WithRequestHeaders(headers http.Header) RequestOption {
	return func(options *InternalRequestOptions) {
		if options == nil || len(headers) == 0 {
			return
		}
		if options.headers == nil {
			options.headers = http.Header{}
		}
		for key, values := range headers {
			for _, value := range values {
				options.headers.Add(key, value)
			}
		}
	}
}

func WithRequestQuery(key string, value string) RequestOption {
	return func(options *InternalRequestOptions) {
		if options == nil || strings.TrimSpace(key) == "" {
			return
		}
		if options.query == nil {
			options.query = url.Values{}
		}
		options.query.Add(key, value)
	}
}

func WithRequestQueryValues(values url.Values) RequestOption {
	return func(options *InternalRequestOptions) {
		if options == nil || len(values) == 0 {
			return
		}
		if options.query == nil {
			options.query = url.Values{}
		}
		for key, list := range values {
			for _, value := range list {
				options.query.Add(key, value)
			}
		}
	}
}

func WithRequestBody(body io.Reader, contentType string) RequestOption {
	return func(options *InternalRequestOptions) {
		if options == nil {
			return
		}
		options.body = body
		options.contentType = strings.TrimSpace(contentType)
	}
}

func WithRequestJSON(params map[string]any) RequestOption {
	return func(options *InternalRequestOptions) {
		if options == nil {
			return
		}
		options.params = params
	}
}

func WithRequestCookie(cookie *http.Cookie) RequestOption {
	return func(options *InternalRequestOptions) {
		if options == nil || cookie == nil {
			return
		}
		options.cookies = append(options.cookies, cookie)
	}
}

func WithRequestCookies(cookies []*http.Cookie) RequestOption {
	return func(options *InternalRequestOptions) {
		if options == nil || len(cookies) == 0 {
			return
		}
		for _, cookie := range cookies {
			if cookie == nil {
				continue
			}
			options.cookies = append(options.cookies, cookie)
		}
	}
}

func WithRequestTimeout(timeout time.Duration) RequestOption {
	return func(options *InternalRequestOptions) {
		if options == nil {
			return
		}
		options.timeout = timeout
		options.timeoutSet = true
	}
}

func WithRequestProxy(proxy string) RequestOption {
	return func(options *InternalRequestOptions) {
		if options == nil {
			return
		}
		options.proxy = strings.TrimSpace(proxy)
		options.proxySet = true
	}
}

func WithRequestProxyURL(proxyURL *url.URL) RequestOption {
	return func(options *InternalRequestOptions) {
		if options == nil {
			return
		}
		if proxyURL == nil {
			options.proxy = ""
			options.proxySet = true
			return
		}
		options.proxy = strings.TrimSpace(proxyURL.String())
		options.proxySet = true
	}
}

func WithRequestBasicAuth(username string, password string) RequestOption {
	return func(options *InternalRequestOptions) {
		if options == nil {
			return
		}
		options.basicUser = username
		options.basicPass = password
		options.basicSet = true
	}
}

func WithRequestBearerAuth(token string) RequestOption {
	return func(options *InternalRequestOptions) {
		if options == nil {
			return
		}
		options.bearerToken = strings.TrimSpace(token)
	}
}

func WithRequestTLSConfig(config *tls.Config) RequestOption {
	return func(options *InternalRequestOptions) {
		if options == nil {
			return
		}
		if config == nil {
			options.tlsConfig = nil
			return
		}
		options.tlsConfig = config.Clone()
	}
}

func NewAPIProtocol(account *Account, httpClient *http.Client) *APIProtocol {
	if account == nil {
		account = NewAccount(nil, APIInfo{}, nil, nil)
	}
	timeout := account.Config.TimeoutValue()
	if timeout <= 0 {
		timeout = protocol.DefaultRequestTimeout
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
	requestOptions ...RequestOption,
) (map[string]any, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	endpoint := p.account.EnsureURL(rawURL)
	method = normalizeRequestMethod(method)
	options := &InternalRequestOptions{
		params:  params,
		headers: http.Header{},
		query:   url.Values{},
	}
	for _, option := range requestOptions {
		if option == nil {
			continue
		}
		option(options)
	}
	if options.timeoutSet && options.timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, options.timeout)
		defer cancel()
	}

	var (
		body    io.Reader
		headers = options.headers
		err     error
	)

	if len(options.query) > 0 {
		endpoint, err = appendQueryValues(endpoint, options.query)
		if err != nil {
			return nil, err
		}
	}

	if options.body != nil {
		body = options.body
		if options.contentType != "" && headers.Get("Content-Type") == "" {
			headers.Set("Content-Type", options.contentType)
		}
	} else if method != http.MethodGet || len(options.params) > 0 {
		var extraHeaders http.Header
		body, extraHeaders, err = encodeJSONBody(options.params)
		if err != nil {
			return nil, err
		}
		for key, values := range extraHeaders {
			for _, value := range values {
				headers.Add(key, value)
			}
		}
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
	for _, cookie := range options.cookies {
		if cookie == nil {
			continue
		}
		request.AddCookie(cookie)
	}
	if options.basicSet {
		request.SetBasicAuth(options.basicUser, options.basicPass)
	}
	if options.bearerToken != "" {
		request.Header.Set("Authorization", "Bearer "+options.bearerToken)
	}

	client, err := p.resolveInternalRequestClient(options)
	if err != nil {
		return nil, err
	}
	payload, err := p.doRequestWithClient(request, client)
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
		if params == nil {
			return nil, errors.New("multipart requires params")
		}
		body, contentType, err := encodeMultipartBody(params)
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

	payload, err := p.requestBytes(ctx, normalizeAPIMethod(method), endpoint, body, headers)
	if err != nil {
		return nil, err
	}
	return payload, nil
}

func (p *APIProtocol) Send(ctx context.Context, evt *event.Event, messageInput any) ([]*message.Message, error) {
	if evt == nil || evt.Channel == nil {
		return nil, errors.New("event cannot be replied to")
	}
	return p.SendMessage(ctx, evt.Channel, messageInput, evt.Referrer)
}

func (p *APIProtocol) SendMessage(
	ctx context.Context,
	channelTarget any,
	messageInput any,
	referrer map[string]any,
) ([]*message.Message, error) {
	channelID, err := resolveChannelID(channelTarget)
	if err != nil {
		return nil, err
	}
	content, err := composeMessageContent(messageInput)
	if err != nil {
		return nil, err
	}
	return p.MessageCreate(ctx, channelID, content, referrer)
}

func (p *APIProtocol) SendPrivateMessage(
	ctx context.Context,
	userTarget any,
	messageInput any,
	referrer map[string]any,
) ([]*message.Message, error) {
	userID, err := resolveUserID(userTarget)
	if err != nil {
		return nil, err
	}
	content, err := composeMessageContent(messageInput)
	if err != nil {
		return nil, err
	}
	channelItem, err := p.UserChannelCreate(ctx, userID, "")
	if err != nil {
		return nil, err
	}
	return p.MessageCreate(ctx, channelItem.Id, content, referrer)
}

func (p *APIProtocol) UpdateMessage(ctx context.Context, channelTarget any, messageID string, messageInput any) error {
	channelID, err := resolveChannelID(channelTarget)
	if err != nil {
		return err
	}
	content, err := composeMessageContent(messageInput)
	if err != nil {
		return err
	}
	return p.MessageUpdate(ctx, channelID, messageID, content)
}

func (p *APIProtocol) MessageCreate(
	ctx context.Context,
	channelID string,
	content string,
	referrer map[string]any,
) ([]*message.Message, error) {
	resp, err := p.CallAPI(ctx, string(protocol.ApiMessageCreate), map[string]any{
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
	resp, err := p.CallAPI(ctx, string(protocol.ApiMessageGet), map[string]any{
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
	_, err := p.CallAPI(ctx, string(protocol.ApiMessageDelete), map[string]any{
		"channel_id": channelID,
		"message_id": messageID,
	}, false, http.MethodPost)
	return err
}

func (p *APIProtocol) MessageUpdate(ctx context.Context, channelID string, messageID string, content string) error {
	_, err := p.CallAPI(ctx, string(protocol.ApiMessageUpdate), map[string]any{
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
) (*model.BidiPaginated[*message.Message], error) {
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

	resp, err := p.CallAPI(ctx, string(protocol.ApiMessageList), map[string]any{
		"channel_id": channelID,
		"next":       nextToken,
		"direction":  direction,
		"limit":      limit,
		"order":      order,
	}, false, http.MethodPost)
	if err != nil {
		return nil, err
	}
	var result model.BidiPaginated[*message.Message]
	if err := decodeJSON(resp, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

func (p *APIProtocol) ChannelGet(ctx context.Context, channelID string) (*channel.Channel, error) {
	resp, err := p.CallAPI(ctx, string(protocol.ApiChannelGet), map[string]any{"channel_id": channelID}, false, http.MethodPost)
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
) *model.PaginatedSeq[*channel.Channel] {
	return paginated.NewPaginatedSeq(ctx, nextToken, func(fetchCtx context.Context, token string) (*model.Paginated[*channel.Channel], error) {
		return p.channelListPage(fetchCtx, guildID, token)
	})
}

func (p *APIProtocol) channelListPage(
	ctx context.Context,
	guildID string,
	nextToken string,
) (*model.Paginated[*channel.Channel], error) {
	resp, err := p.CallAPI(ctx, string(protocol.ApiChannelList), map[string]any{
		"guild_id": guildID,
		"next":     nextToken,
	}, false, http.MethodPost)
	if err != nil {
		return nil, err
	}
	var result model.Paginated[*channel.Channel]
	if err := decodeJSON(resp, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

func (p *APIProtocol) ChannelCreate(ctx context.Context, guildID string, data *channel.Channel) (*channel.Channel, error) {
	resp, err := p.CallAPI(ctx, string(protocol.ApiChannelCreate), map[string]any{"guild_id": guildID, "data": data}, false, http.MethodPost)
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
	_, err := p.CallAPI(ctx, string(protocol.ApiChannelUpdate), map[string]any{"channel_id": channelID, "data": data}, false, http.MethodPost)
	return err
}

func (p *APIProtocol) ChannelDelete(ctx context.Context, channelID string) error {
	_, err := p.CallAPI(ctx, string(protocol.ApiChannelDelete), map[string]any{"channel_id": channelID}, false, http.MethodPost)
	return err
}

func (p *APIProtocol) ChannelMute(ctx context.Context, channelID string, duration time.Duration) error {
	_, err := p.CallAPI(ctx, string(protocol.ApiChannelMute), map[string]any{
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
	resp, err := p.CallAPI(ctx, string(protocol.ApiUserChannelCreate), params, false, http.MethodPost)
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
	resp, err := p.CallAPI(ctx, string(protocol.ApiGuildGet), map[string]any{"guild_id": guildID}, false, http.MethodPost)
	if err != nil {
		return nil, err
	}
	var result guild.Guild
	if err := decodeJSON(resp, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

func (p *APIProtocol) GuildList(ctx context.Context, nextToken string) *model.PaginatedSeq[*guild.Guild] {
	return paginated.NewPaginatedSeq(ctx, nextToken, func(fetchCtx context.Context, token string) (*model.Paginated[*guild.Guild], error) {
		return p.guildListPage(fetchCtx, token)
	})
}

func (p *APIProtocol) guildListPage(ctx context.Context, nextToken string) (*model.Paginated[*guild.Guild], error) {
	resp, err := p.CallAPI(ctx, string(protocol.ApiGuildList), map[string]any{"next": nextToken}, false, http.MethodPost)
	if err != nil {
		return nil, err
	}
	var result model.Paginated[*guild.Guild]
	if err := decodeJSON(resp, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

func (p *APIProtocol) GuildApprove(ctx context.Context, requestID string, approve bool, comment string) error {
	_, err := p.CallAPI(ctx, string(protocol.ApiGuildApprove), map[string]any{
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
) *model.PaginatedSeq[*guildmember.GuildMember] {
	return paginated.NewPaginatedSeq(ctx, nextToken, func(fetchCtx context.Context, token string) (*model.Paginated[*guildmember.GuildMember], error) {
		return p.guildMemberListPage(fetchCtx, guildID, token)
	})
}

func (p *APIProtocol) guildMemberListPage(
	ctx context.Context,
	guildID string,
	nextToken string,
) (*model.Paginated[*guildmember.GuildMember], error) {
	resp, err := p.CallAPI(ctx, string(protocol.ApiGuildMemberList), map[string]any{
		"guild_id": guildID,
		"next":     nextToken,
	}, false, http.MethodPost)
	if err != nil {
		return nil, err
	}
	var result model.Paginated[*guildmember.GuildMember]
	if err := decodeJSON(resp, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

func (p *APIProtocol) GuildMemberGet(ctx context.Context, guildID string, userID string) (*guildmember.GuildMember, error) {
	resp, err := p.CallAPI(ctx, string(protocol.ApiGuildMemberGet), map[string]any{
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
	_, err := p.CallAPI(ctx, string(protocol.ApiGuildMemberKick), map[string]any{
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
	_, err := p.CallAPI(ctx, string(protocol.ApiGuildMemberMute), map[string]any{
		"guild_id": guildID,
		"user_id":  userID,
		"duration": duration.Milliseconds(),
	}, false, http.MethodPost)
	return err
}

func (p *APIProtocol) GuildMemberApprove(ctx context.Context, requestID string, approve bool, comment string) error {
	_, err := p.CallAPI(ctx, string(protocol.ApiGuildMemberApprove), map[string]any{
		"message_id": requestID,
		"approve":    approve,
		"comment":    comment,
	}, false, http.MethodPost)
	return err
}

func (p *APIProtocol) GuildMemberRoleSet(ctx context.Context, guildID string, userID string, roleID string) error {
	_, err := p.CallAPI(ctx, string(protocol.ApiGuildMemberRoleSet), map[string]any{
		"guild_id": guildID,
		"user_id":  userID,
		"role_id":  roleID,
	}, false, http.MethodPost)
	return err
}

func (p *APIProtocol) GuildMemberRoleUnset(ctx context.Context, guildID string, userID string, roleID string) error {
	_, err := p.CallAPI(ctx, string(protocol.ApiGuildMemberRoleUnset), map[string]any{
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
) *model.PaginatedSeq[*guildrole.GuildRole] {
	return paginated.NewPaginatedSeq(ctx, nextToken, func(fetchCtx context.Context, token string) (*model.Paginated[*guildrole.GuildRole], error) {
		return p.guildRoleListPage(fetchCtx, guildID, token)
	})
}

func (p *APIProtocol) guildRoleListPage(
	ctx context.Context,
	guildID string,
	nextToken string,
) (*model.Paginated[*guildrole.GuildRole], error) {
	resp, err := p.CallAPI(ctx, string(protocol.ApiGuildRoleList), map[string]any{
		"guild_id": guildID,
		"next":     nextToken,
	}, false, http.MethodPost)
	if err != nil {
		return nil, err
	}
	var result model.Paginated[*guildrole.GuildRole]
	if err := decodeJSON(resp, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

func (p *APIProtocol) GuildRoleCreate(ctx context.Context, guildID string, role *guildrole.GuildRole) (*guildrole.GuildRole, error) {
	resp, err := p.CallAPI(ctx, string(protocol.ApiGuildRoleCreate), map[string]any{"guild_id": guildID, "role": role}, false, http.MethodPost)
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
	_, err := p.CallAPI(ctx, string(protocol.ApiGuildRoleUpdate), map[string]any{
		"guild_id": guildID,
		"role_id":  roleID,
		"role":     role,
	}, false, http.MethodPost)
	return err
}

func (p *APIProtocol) GuildRoleDelete(ctx context.Context, guildID string, roleID string) error {
	_, err := p.CallAPI(ctx, string(protocol.ApiGuildRoleDelete), map[string]any{
		"guild_id": guildID,
		"role_id":  roleID,
	}, false, http.MethodPost)
	return err
}

func (p *APIProtocol) ReactionCreate(ctx context.Context, channelID string, messageID string, emoji string) error {
	_, err := p.CallAPI(ctx, string(protocol.ApiReactionCreate), map[string]any{
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
	_, err := p.CallAPI(ctx, string(protocol.ApiReactionDelete), params, false, http.MethodPost)
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
	_, err := p.CallAPI(ctx, string(protocol.ApiReactionClear), params, false, http.MethodPost)
	return err
}

func (p *APIProtocol) ReactionList(
	ctx context.Context,
	channelID string,
	messageID string,
	emoji string,
	nextToken string,
) *model.PaginatedSeq[*user.User] {
	return paginated.NewPaginatedSeq(ctx, nextToken, func(fetchCtx context.Context, token string) (*model.Paginated[*user.User], error) {
		return p.reactionListPage(fetchCtx, channelID, messageID, emoji, token)
	})
}

func (p *APIProtocol) reactionListPage(
	ctx context.Context,
	channelID string,
	messageID string,
	emoji string,
	nextToken string,
) (*model.Paginated[*user.User], error) {
	resp, err := p.CallAPI(ctx, string(protocol.ApiReactionList), map[string]any{
		"channel_id": channelID,
		"message_id": messageID,
		"emoji":      emoji,
		"next":       nextToken,
	}, false, http.MethodPost)
	if err != nil {
		return nil, err
	}
	var result model.Paginated[*user.User]
	if err := decodeJSON(resp, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

func (p *APIProtocol) LoginGet(ctx context.Context) (*login.Login, error) {
	resp, err := p.CallAPI(ctx, string(protocol.ApiLoginGet), map[string]any{}, false, http.MethodPost)
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
	resp, err := p.CallAPI(ctx, string(protocol.ApiUserGet), map[string]any{"user_id": userID}, false, http.MethodPost)
	if err != nil {
		return nil, err
	}
	var result user.User
	if err := decodeJSON(resp, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

func (p *APIProtocol) FriendList(ctx context.Context, nextToken string) *model.PaginatedSeq[*model.Friend] {
	return paginated.NewPaginatedSeq(ctx, nextToken, func(fetchCtx context.Context, token string) (*model.Paginated[*model.Friend], error) {
		return p.friendListPage(fetchCtx, token)
	})
}

func (p *APIProtocol) friendListPage(ctx context.Context, nextToken string) (*model.Paginated[*model.Friend], error) {
	resp, err := p.CallAPI(ctx, string(protocol.ApiFriendList), map[string]any{"next": nextToken}, false, http.MethodPost)
	if err != nil {
		return nil, err
	}
	var result model.Paginated[*model.Friend]
	if err := decodeJSON(resp, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

func (p *APIProtocol) FriendDelete(ctx context.Context, userID string) error {
	_, err := p.CallAPI(ctx, string(protocol.ApiFriendDelete), map[string]any{"user_id": userID}, false, http.MethodPost)
	return err
}

func (p *APIProtocol) FriendApprove(ctx context.Context, requestID string, approve bool, comment string) error {
	_, err := p.CallAPI(ctx, string(protocol.ApiFriendApprove), map[string]any{
		"message_id": requestID,
		"approve":    approve,
		"comment":    comment,
	}, false, http.MethodPost)
	return err
}

func (p *APIProtocol) Internal(ctx context.Context, action string, method string, params map[string]any) (any, error) {
	internalAction := protocol.NormalizeInternalApi(action)
	if internalAction == "" {
		return nil, errors.New("internal action cannot be empty")
	}
	resp, err := p.CallAPI(ctx, internalAction, params, false, method)
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
	resp, err := p.CallAPI(ctx, string(protocol.ApiMetaGet), map[string]any{}, false, http.MethodPost)
	if err != nil {
		return nil, err
	}
	var result meta.Meta
	if err := decodeJSON(resp, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

func (p *APIProtocol) AdminLoginList(ctx context.Context) ([]*login.LoginPartial, error) {
	resp, err := p.CallAPI(ctx, string(protocol.ApiAdminLoginList), map[string]any{}, false, http.MethodPost)
	if err != nil {
		return nil, err
	}
	var result []*login.LoginPartial
	if err := decodeJSON(resp, &result); err != nil {
		return nil, err
	}
	return result, nil
}

func (p *APIProtocol) WebhookCreate(ctx context.Context, endpoint string, token string) error {
	_, err := p.CallAPI(ctx, string(protocol.ApiMetaWebhookCreate), map[string]any{"url": endpoint, "token": token}, false, http.MethodPost)
	return err
}

func (p *APIProtocol) WebhookDelete(ctx context.Context, endpoint string) error {
	_, err := p.CallAPI(ctx, string(protocol.ApiMetaWebhookDelete), map[string]any{"url": endpoint}, false, http.MethodPost)
	return err
}

func (p *APIProtocol) UploadCreateNamed(ctx context.Context, uploads map[string]Upload) (map[string]string, error) {
	params := make(map[string]any, len(uploads))
	for name, upload := range uploads {
		params[name] = upload
	}
	resp, err := p.CallAPI(ctx, string(protocol.ApiUploadCreate), params, true, http.MethodPost)
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
	return p.doRequest(request)
}

func (p *APIProtocol) doRequest(request *http.Request) ([]byte, error) {
	return p.doRequestWithClient(request, p.client)
}

func (p *APIProtocol) doRequestWithClient(request *http.Request, client *http.Client) ([]byte, error) {
	if request == nil {
		return nil, errors.New("request cannot be nil")
	}
	if client == nil {
		client = &http.Client{Timeout: p.timeout}
	}

	response, err := client.Do(request)
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
	return nil, errorFromStatusCode(response.StatusCode, payload)
}

func (p *APIProtocol) resolveInternalRequestClient(options *InternalRequestOptions) (*http.Client, error) {
	if options == nil {
		return p.client, nil
	}
	requiresTransportClone := options.proxySet || options.tlsConfig != nil
	if !requiresTransportClone {
		return p.client, nil
	}

	baseClient := p.client
	if baseClient == nil {
		baseClient = &http.Client{Timeout: p.timeout}
	}
	clonedClient := *baseClient

	clonedTransport, err := cloneHTTPTransport(baseClient.Transport)
	if err != nil {
		return nil, err
	}
	if options.proxySet {
		if options.proxy == "" {
			clonedTransport.Proxy = nil
		} else {
			proxyURL, parseErr := url.Parse(options.proxy)
			if parseErr != nil {
				return nil, fmt.Errorf("invalid request proxy url %q: %w", options.proxy, parseErr)
			}
			clonedTransport.Proxy = http.ProxyURL(proxyURL)
		}
	}
	if options.tlsConfig != nil {
		clonedTransport.TLSClientConfig = options.tlsConfig.Clone()
	}

	clonedClient.Transport = clonedTransport
	return &clonedClient, nil
}

func cloneHTTPTransport(roundTripper http.RoundTripper) (*http.Transport, error) {
	if roundTripper == nil {
		defaultTransport, ok := http.DefaultTransport.(*http.Transport)
		if !ok {
			return &http.Transport{}, nil
		}
		return defaultTransport.Clone(), nil
	}
	transport, ok := roundTripper.(*http.Transport)
	if !ok {
		return nil, fmt.Errorf("unsupported http transport type %T", roundTripper)
	}
	return transport.Clone(), nil
}

func (p *APIProtocol) apiHeaders() http.Header {
	headers := http.Header{}
	headers.Set("Content-Type", "application/json")
	protocol.SetBearer(headers, p.account.Config.TokenValue())
	protocol.SetIdentityHeaders(headers, p.account.Platform(), p.account.SelfID())
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

type multipartPart struct {
	name        string
	value       []byte
	filename    string
	contentType string
	asFile      bool
}

func resolveChannelID(target any) (string, error) {
	switch typed := target.(type) {
	case string:
		channelID := strings.TrimSpace(typed)
		if channelID == "" {
			return "", errors.New("channel id cannot be empty")
		}
		return channelID, nil
	case *channel.Channel:
		if typed == nil {
			return "", errors.New("channel cannot be nil")
		}
		channelID := strings.TrimSpace(typed.Id)
		if channelID == "" {
			return "", errors.New("channel id cannot be empty")
		}
		return channelID, nil
	case channel.Channel:
		channelID := strings.TrimSpace(typed.Id)
		if channelID == "" {
			return "", errors.New("channel id cannot be empty")
		}
		return channelID, nil
	default:
		return "", fmt.Errorf("unsupported channel target type %T", target)
	}
}

func resolveUserID(target any) (string, error) {
	switch typed := target.(type) {
	case string:
		userID := strings.TrimSpace(typed)
		if userID == "" {
			return "", errors.New("user id cannot be empty")
		}
		return userID, nil
	case *user.User:
		if typed == nil {
			return "", errors.New("user cannot be nil")
		}
		userID := strings.TrimSpace(typed.Id)
		if userID == "" {
			return "", errors.New("user id cannot be empty")
		}
		return userID, nil
	case user.User:
		userID := strings.TrimSpace(typed.Id)
		if userID == "" {
			return "", errors.New("user id cannot be empty")
		}
		return userID, nil
	default:
		return "", fmt.Errorf("unsupported user target type %T", target)
	}
}

func composeMessageContent(input any) (string, error) {
	var builder strings.Builder
	if err := appendMessageContent(&builder, input); err != nil {
		return "", err
	}
	return builder.String(), nil
}

func appendMessageContent(builder *strings.Builder, value any) error {
	switch typed := value.(type) {
	case nil:
		return nil
	case string:
		builder.WriteString(typed)
		return nil
	case []string:
		for _, item := range typed {
			builder.WriteString(item)
		}
		return nil
	case []byte:
		builder.Write(typed)
		return nil
	case []rune:
		builder.WriteString(string(typed))
		return nil
	case element.Element:
		builder.WriteString(typed.MarshalXHTML(false))
		return nil
	case []element.Element:
		for _, item := range typed {
			if item == nil {
				continue
			}
			builder.WriteString(item.MarshalXHTML(false))
		}
		return nil
	case fmt.Stringer:
		builder.WriteString(typed.String())
		return nil
	case []fmt.Stringer:
		for _, item := range typed {
			if item == nil {
				continue
			}
			builder.WriteString(item.String())
		}
		return nil
	case []any:
		for _, item := range typed {
			if err := appendMessageContent(builder, item); err != nil {
				return err
			}
		}
		return nil
	case bool, int, int8, int16, int32, int64, uint, uint8, uint16, uint32, uint64, uintptr, float32, float64:
		builder.WriteString(fmt.Sprint(typed))
		return nil
	default:
		refValue := reflect.ValueOf(value)
		if refValue.IsValid() && (refValue.Kind() == reflect.Slice || refValue.Kind() == reflect.Array) {
			for index := 0; index < refValue.Len(); index++ {
				if err := appendMessageContent(builder, refValue.Index(index).Interface()); err != nil {
					return err
				}
			}
			return nil
		}
		return fmt.Errorf("unsupported message type %T", value)
	}
}

func encodeMultipartBody(params map[string]any) (io.Reader, string, error) {
	if params == nil {
		return nil, "", errors.New("multipart requires params")
	}
	buffer := &bytes.Buffer{}
	writer := multipart.NewWriter(buffer)

	for name, raw := range params {
		part, err := normalizeMultipartPart(name, raw)
		if err != nil {
			_ = writer.Close()
			return nil, "", err
		}
		if part.asFile {
			header := textproto.MIMEHeader{}
			header.Set("Content-Disposition", fmt.Sprintf(`form-data; name=%q; filename=%q`, part.name, part.filename))
			header.Set("Content-Type", part.contentType)

			partWriter, createErr := writer.CreatePart(header)
			if createErr != nil {
				_ = writer.Close()
				return nil, "", createErr
			}
			if _, writeErr := partWriter.Write(part.value); writeErr != nil {
				_ = writer.Close()
				return nil, "", writeErr
			}
			continue
		}
		if err := writer.WriteField(part.name, string(part.value)); err != nil {
			_ = writer.Close()
			return nil, "", err
		}
	}

	if err := writer.Close(); err != nil {
		return nil, "", err
	}
	return buffer, writer.FormDataContentType(), nil
}

func normalizeMultipartPart(name string, raw any) (multipartPart, error) {
	switch typed := raw.(type) {
	case nil:
		return multipartPart{name: name, value: []byte{}}, nil
	case Upload:
		upload := typed.normalized(name)
		return multipartPart{
			name:        name,
			value:       append([]byte(nil), upload.Value...),
			filename:    upload.Filename,
			contentType: upload.ContentType,
			asFile:      true,
		}, nil
	case *Upload:
		if typed == nil {
			return multipartPart{name: name, value: []byte{}}, nil
		}
		upload := typed.normalized(name)
		return multipartPart{
			name:        name,
			value:       append([]byte(nil), upload.Value...),
			filename:    upload.Filename,
			contentType: upload.ContentType,
			asFile:      true,
		}, nil
	case map[string]any:
		return normalizeMultipartMap(name, typed)
	case map[string]string:
		converted := make(map[string]any, len(typed))
		for key, value := range typed {
			converted[key] = value
		}
		return normalizeMultipartMap(name, converted)
	case []byte:
		return multipartPart{
			name:        name,
			value:       append([]byte(nil), typed...),
			filename:    name,
			contentType: "application/octet-stream",
			asFile:      true,
		}, nil
	case io.Reader:
		data, err := io.ReadAll(typed)
		if err != nil {
			return multipartPart{}, fmt.Errorf("multipart param %q read failed: %w", name, err)
		}
		return multipartPart{
			name:        name,
			value:       data,
			filename:    name,
			contentType: "application/octet-stream",
			asFile:      true,
		}, nil
	case string:
		return multipartPart{name: name, value: []byte(typed)}, nil
	case fmt.Stringer:
		return multipartPart{name: name, value: []byte(typed.String())}, nil
	case bool, int, int8, int16, int32, int64, uint, uint8, uint16, uint32, uint64, uintptr, float32, float64:
		return multipartPart{name: name, value: []byte(fmt.Sprint(typed))}, nil
	default:
		return multipartPart{}, fmt.Errorf("multipart param %q has unsupported type %T", name, raw)
	}
}

func normalizeMultipartMap(name string, fields map[string]any) (multipartPart, error) {
	value, ok := fields["value"]
	if !ok {
		return multipartPart{}, fmt.Errorf("multipart param %q is missing \"value\"", name)
	}

	filename := stringValue(fields["filename"])
	contentType := stringValue(fields["content_type"])
	if contentType == "" {
		contentType = stringValue(fields["contentType"])
	}

	switch typed := value.(type) {
	case nil:
		if filename == "" && contentType == "" {
			return multipartPart{name: name, value: []byte{}}, nil
		}
		if filename == "" {
			filename = name
		}
		if contentType == "" {
			contentType = "application/octet-stream"
		}
		return multipartPart{
			name:        name,
			value:       []byte{},
			filename:    filename,
			contentType: contentType,
			asFile:      true,
		}, nil
	case Upload:
		upload := typed
		if filename != "" {
			upload.Filename = filename
		}
		if contentType != "" {
			upload.ContentType = contentType
		}
		normalized := upload.normalized(name)
		return multipartPart{
			name:        name,
			value:       append([]byte(nil), normalized.Value...),
			filename:    normalized.Filename,
			contentType: normalized.ContentType,
			asFile:      true,
		}, nil
	case *Upload:
		if typed == nil {
			return multipartPart{name: name, value: []byte{}}, nil
		}
		upload := *typed
		if filename != "" {
			upload.Filename = filename
		}
		if contentType != "" {
			upload.ContentType = contentType
		}
		normalized := upload.normalized(name)
		return multipartPart{
			name:        name,
			value:       append([]byte(nil), normalized.Value...),
			filename:    normalized.Filename,
			contentType: normalized.ContentType,
			asFile:      true,
		}, nil
	case []byte:
		if filename == "" {
			filename = name
		}
		if contentType == "" {
			contentType = "application/octet-stream"
		}
		return multipartPart{
			name:        name,
			value:       append([]byte(nil), typed...),
			filename:    filename,
			contentType: contentType,
			asFile:      true,
		}, nil
	case io.Reader:
		data, err := io.ReadAll(typed)
		if err != nil {
			return multipartPart{}, fmt.Errorf("multipart param %q read failed: %w", name, err)
		}
		if filename == "" {
			filename = name
		}
		if contentType == "" {
			contentType = "application/octet-stream"
		}
		return multipartPart{
			name:        name,
			value:       data,
			filename:    filename,
			contentType: contentType,
			asFile:      true,
		}, nil
	case string:
		if filename == "" && contentType == "" {
			return multipartPart{name: name, value: []byte(typed)}, nil
		}
		if filename == "" {
			filename = name
		}
		if contentType == "" {
			contentType = "text/plain; charset=utf-8"
		}
		return multipartPart{
			name:        name,
			value:       []byte(typed),
			filename:    filename,
			contentType: contentType,
			asFile:      true,
		}, nil
	case bool, int, int8, int16, int32, int64, uint, uint8, uint16, uint32, uint64, uintptr, float32, float64:
		text := fmt.Sprint(typed)
		if filename == "" && contentType == "" {
			return multipartPart{name: name, value: []byte(text)}, nil
		}
		if filename == "" {
			filename = name
		}
		if contentType == "" {
			contentType = "text/plain; charset=utf-8"
		}
		return multipartPart{
			name:        name,
			value:       []byte(text),
			filename:    filename,
			contentType: contentType,
			asFile:      true,
		}, nil
	default:
		if str, ok := value.(fmt.Stringer); ok {
			text := str.String()
			if filename == "" && contentType == "" {
				return multipartPart{name: name, value: []byte(text)}, nil
			}
			if filename == "" {
				filename = name
			}
			if contentType == "" {
				contentType = "text/plain; charset=utf-8"
			}
			return multipartPart{
				name:        name,
				value:       []byte(text),
				filename:    filename,
				contentType: contentType,
				asFile:      true,
			}, nil
		}
		return multipartPart{}, fmt.Errorf("multipart param %q has unsupported \"value\" type %T", name, value)
	}
}

func stringValue(value any) string {
	switch typed := value.(type) {
	case nil:
		return ""
	case string:
		return strings.TrimSpace(typed)
	case fmt.Stringer:
		return strings.TrimSpace(typed.String())
	default:
		return strings.TrimSpace(fmt.Sprint(typed))
	}
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
	_, err := protocol.DecodeJSONBytes(payload, target)
	return err
}

func normalizeRequestMethod(method string) string {
	return normalizeMethodWithDefault(method, http.MethodGet)
}

func normalizeAPIMethod(method string) string {
	return normalizeMethodWithDefault(method, http.MethodPost)
}

func normalizeMethodWithDefault(method string, defaultMethod string) string {
	method = strings.TrimSpace(strings.ToUpper(method))
	if method == "" {
		return defaultMethod
	}
	return method
}

func appendQueryValues(rawURL string, values url.Values) (string, error) {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return "", err
	}
	query := parsed.Query()
	for key, list := range values {
		for _, value := range list {
			query.Add(key, value)
		}
	}
	parsed.RawQuery = query.Encode()
	return parsed.String(), nil
}
