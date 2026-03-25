package qq

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/WindowsSov8forUs/botgo-plus/dto"
	"github.com/satori-protocol-go/satori-go/pkg/satori/adapter/qq/convert"
	"github.com/satori-protocol-go/satori-go/pkg/satori/model"
	"github.com/satori-protocol-go/satori-go/pkg/satori/model/channel"
	"github.com/satori-protocol-go/satori-go/pkg/satori/model/guild"
	"github.com/satori-protocol-go/satori-go/pkg/satori/model/guildmember"
	"github.com/satori-protocol-go/satori-go/pkg/satori/model/guildrole"
	"github.com/satori-protocol-go/satori-go/pkg/satori/model/message"
	"github.com/satori-protocol-go/satori-go/pkg/satori/model/user"
	"github.com/satori-protocol-go/satori-go/pkg/satori/protocol"
	"github.com/satori-protocol-go/satori-go/pkg/satori/server"
)

func (a *Adapter) handleChannelGet(request *server.Request[server.ChannelParam]) (any, error) {
	if request.Platform != "qqguild" {
		return nil, server.NotFound("channel.get is not supported in current platform")
	}
	state, err := a.resolveRequestState(request.Origin, request.SelfID)
	if err != nil {
		return nil, err
	}
	api := state.apiV1
	channelID := request.Params.ChannelID

	fetched, err := api.Channel(requestContext(request.Origin), convert.SplitChannelCompositeID(channelID))
	if err != nil {
		return nil, err
	}
	if fetched == nil {
		return nil, server.NotFound("channel not found")
	}
	return convert.ChannelFromDTO(fetched), nil
}

func (a *Adapter) handleChannelList(request *server.Request[server.ChannelListParam]) (any, error) {
	if request.Platform != "qqguild" {
		return nil, server.NotFound("channel.list is not supported in current platform")
	}
	state, err := a.resolveRequestState(request.Origin, request.SelfID)
	if err != nil {
		return nil, err
	}
	api := state.apiV1
	guildID := request.Params.GuildID

	channelsValue, err := api.Channels(requestContext(request.Origin), convert.SplitGuildCompositeID(guildID))
	if err != nil {
		return nil, err
	}
	data := make([]*channel.Channel, 0, len(channelsValue))
	for _, item := range channelsValue {
		data = append(data, convert.ChannelFromDTO(item))
	}
	return &model.Paginated[*channel.Channel]{Data: data}, nil
}

func (a *Adapter) handleChannelCreate(request *server.Request[server.ChannelCreateParam]) (any, error) {
	if request.Platform != "qqguild" {
		return nil, server.NotFound("channel.create is not supported in current platform")
	}
	state, err := a.resolveRequestState(request.Origin, request.SelfID)
	if err != nil {
		return nil, err
	}
	api := state.apiV1
	guildID := request.Params.GuildID

	created, err := api.PostChannel(
		requestContext(request.Origin),
		convert.SplitGuildCompositeID(guildID),
		convert.ParseChannelValue(request.Params.Data),
	)
	if err != nil {
		return nil, err
	}
	if created == nil {
		return nil, server.NotFound("channel not created")
	}
	return convert.ChannelFromDTO(created), nil
}

func (a *Adapter) handleChannelUpdate(request *server.Request[server.ChannelUpdateParam]) (any, error) {
	if request.Platform != "qqguild" {
		return nil, server.NotFound("channel.update is not supported in current platform")
	}
	state, err := a.resolveRequestState(request.Origin, request.SelfID)
	if err != nil {
		return nil, err
	}
	api := state.apiV1
	channelID := request.Params.ChannelID

	updated, err := api.PatchChannel(
		requestContext(request.Origin),
		convert.SplitChannelCompositeID(channelID),
		convert.ParseChannelValue(request.Params.Data),
	)
	if err != nil {
		return nil, err
	}
	if updated == nil {
		return map[string]any{}, nil
	}
	return convert.ChannelFromDTO(updated), nil
}

func (a *Adapter) handleChannelDelete(request *server.Request[server.ChannelParam]) (any, error) {
	if request.Platform != "qqguild" {
		return nil, server.NotFound("channel.delete is not supported in current platform")
	}
	state, err := a.resolveRequestState(request.Origin, request.SelfID)
	if err != nil {
		return nil, err
	}
	api := state.apiV1
	channelID := request.Params.ChannelID

	if err := api.DeleteChannel(requestContext(request.Origin), convert.SplitChannelCompositeID(channelID)); err != nil {
		return nil, err
	}
	return map[string]any{}, nil
}

func (a *Adapter) handleChannelMute(_ *server.Request[server.ChannelMuteParam]) (any, error) {
	return nil, server.NotFound("channel.mute is not supported")
}

func (a *Adapter) handleGuildGet(request *server.Request[server.GuildGetParam]) (any, error) {
	if request.Platform != "qqguild" {
		return nil, server.NotFound("guild.get is not supported in current platform")
	}
	state, err := a.resolveRequestState(request.Origin, request.SelfID)
	if err != nil {
		return nil, err
	}
	api := state.apiV1
	guildID := request.Params.GuildID

	fetched, err := api.Guild(requestContext(request.Origin), convert.SplitGuildCompositeID(guildID))
	if err != nil {
		return nil, err
	}
	if fetched == nil {
		return nil, server.NotFound("guild not found")
	}
	return convert.GuildFromDTO(fetched), nil
}

func (a *Adapter) handleGuildList(request *server.Request[server.GuildListParam]) (any, error) {
	if request.Platform != "qqguild" {
		return nil, server.NotFound("guild.list is not supported in current platform")
	}
	state, err := a.resolveRequestState(request.Origin, request.SelfID)
	if err != nil {
		return nil, err
	}
	api := state.apiV1

	pager := &dto.GuildPager{Limit: "100"}
	if nextValue, ok := request.Params.Next.Get(); ok {
		next := nextValue
		if next != "" {
			pager.After = next
		}
	}

	items, err := api.MeGuilds(requestContext(request.Origin), pager)
	if err != nil {
		return nil, err
	}
	data := make([]*guild.Guild, 0, len(items))
	for _, item := range items {
		data = append(data, convert.GuildFromDTO(item))
	}

	response := &model.Paginated[*guild.Guild]{Data: data}
	if len(data) > 0 {
		response.Next = data[len(data)-1].Id
	}
	return response, nil
}

func (a *Adapter) handleInternalRoute(request *server.Request[server.InternalParam]) (any, error) {
	path := strings.TrimPrefix(request.Action, "internal/")
	params := request.Params
	if params == nil {
		params = map[string]any{}
	}

	resp, err := a.HandleInternal(server.Request[map[string]any]{
		Origin:   request.Origin,
		Action:   request.Action,
		Params:   params,
		Platform: request.Platform,
		SelfID:   request.SelfID,
	}, path)
	if err != nil {
		return nil, err
	}
	if resp == nil {
		return map[string]any{}, nil
	}
	return resp, nil
}

func (a *Adapter) HandleInternal(
	request server.Request[map[string]any],
	path string,
) (*server.Response, error) {
	path = strings.TrimSpace(path)
	if !strings.HasPrefix(path, "_api") {
		return nil, server.NotFound("internal path is not supported")
	}

	action := strings.TrimPrefix(path, "_api")
	action = strings.TrimPrefix(action, "/")
	if action == "" {
		return nil, server.BadRequest("internal api action is required")
	}

	method := http.MethodGet
	ctx := context.Background()
	if request.Origin != nil {
		method = request.Origin.Method
		ctx = request.Origin.Context()
	}
	if strings.TrimSpace(method) == "" {
		method = http.MethodGet
	}

	params := request.Params
	if params == nil {
		params = map[string]any{}
	}

	body, contentType, status, err := a.callRawAPI(ctx, request.SelfID, method, action, params)
	if err != nil {
		return nil, err
	}
	response := server.NewResponse(status, body)
	if contentType != "" {
		response.Header.Set("Content-Type", contentType)
	}
	return response, nil
}

func (a *Adapter) callRawAPI(
	ctx context.Context,
	selfID string,
	method string,
	action string,
	params map[string]any,
) ([]byte, string, int, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	method = strings.ToUpper(strings.TrimSpace(method))
	if method == "" {
		method = http.MethodGet
	}

	targetURL := strings.TrimRight(a.apiBaseURL(), "/") + "/" + strings.TrimLeft(action, "/")
	var body io.Reader
	if method == http.MethodGet || method == http.MethodDelete {
		query := url.Values{}
		for key, value := range params {
			switch typed := value.(type) {
			case nil:
				continue
			case []string:
				for _, item := range typed {
					query.Add(key, item)
				}
			case []any:
				for _, item := range typed {
					query.Add(key, fmt.Sprint(item))
				}
			default:
				query.Set(key, fmt.Sprint(value))
			}
		}
		if encoded := query.Encode(); encoded != "" {
			targetURL += "?" + encoded
		}
	} else {
		payload, err := json.Marshal(params)
		if err != nil {
			return nil, "", 0, err
		}
		body = bytes.NewReader(payload)
	}

	req, err := http.NewRequestWithContext(ctx, method, targetURL, body)
	if err != nil {
		return nil, "", 0, err
	}

	authorization, err := a.currentAuthorizationToken(ctx, selfID)
	if err != nil {
		return nil, "", 0, err
	}
	req.Header.Set("Authorization", authorization)
	req.Header.Set("X-Union-Appid", a.appID)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := a.httpClient.Do(req)
	if err != nil {
		return nil, "", 0, err
	}
	defer resp.Body.Close()

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, "", 0, err
	}
	if resp.StatusCode >= 400 {
		return nil, "", 0, server.NewActionError(resp.StatusCode, string(data), nil)
	}
	return data, resp.Header.Get("Content-Type"), resp.StatusCode, nil
}

func (a *Adapter) apiBaseURL() string {
	if a.cfg.Sandbox {
		return qqSandboxAPIBaseURL
	}
	return qqAPIBaseURL
}

func (a *Adapter) handleLoginGet(request *server.Request[server.LoginGetParam]) (any, error) {
	if err := a.ensureLogins(requestContext(request.Origin)); err != nil {
		return nil, err
	}
	login := a.findLogin(request.Platform, request.SelfID)
	if login == nil {
		return nil, server.NotFound("login not found")
	}
	return login, nil
}

func (a *Adapter) handleGuildMemberGet(request *server.Request[server.GuildMemberGetParam]) (any, error) {
	if request.Platform != "qqguild" {
		return nil, server.NotFound("guild.member.get is not supported in current platform")
	}
	state, err := a.resolveRequestState(request.Origin, request.SelfID)
	if err != nil {
		return nil, err
	}
	api := state.apiV1
	guildID := request.Params.GuildID
	userID := request.Params.UserID

	member, err := api.GuildMember(requestContext(request.Origin), convert.SplitGuildCompositeID(guildID), userID)
	if err != nil {
		return nil, err
	}
	if member == nil {
		return nil, server.NotFound("member not found")
	}
	return convert.MemberFromDTO(member), nil
}

func (a *Adapter) handleGuildMemberList(request *server.Request[server.GuildListByGuildParam]) (any, error) {
	if request.Platform != "qqguild" {
		return nil, server.NotFound("guild.member.list is not supported in current platform")
	}
	state, err := a.resolveRequestState(request.Origin, request.SelfID)
	if err != nil {
		return nil, err
	}
	api := state.apiV1
	guildID := request.Params.GuildID

	pager := &dto.GuildMembersPager{After: "0", Limit: "400"}
	if nextValue, ok := request.Params.Next.Get(); ok {
		next := nextValue
		if next != "" {
			pager.After = next
		}
	}

	items, err := api.GuildMembers(requestContext(request.Origin), convert.SplitGuildCompositeID(guildID), pager)
	if err != nil {
		return nil, err
	}
	data := make([]*guildmember.GuildMember, 0, len(items))
	for _, item := range items {
		data = append(data, convert.MemberFromDTO(item))
	}
	response := &model.Paginated[*guildmember.GuildMember]{Data: data}
	if len(data) > 0 && data[len(data)-1] != nil && data[len(data)-1].User != nil {
		response.Next = data[len(data)-1].User.Id
	}
	return response, nil
}

func (a *Adapter) handleGuildMemberKick(request *server.Request[server.GuildMemberKickParam]) (any, error) {
	if request.Platform != "qqguild" {
		return nil, server.NotFound("guild.member.kick is not supported in current platform")
	}
	state, err := a.resolveRequestState(request.Origin, request.SelfID)
	if err != nil {
		return nil, err
	}
	api := state.apiV1
	guildID := request.Params.GuildID
	userID := request.Params.UserID

	if err := api.DeleteGuildMember(requestContext(request.Origin), convert.SplitGuildCompositeID(guildID), userID); err != nil {
		return nil, err
	}
	return map[string]any{}, nil
}

func (a *Adapter) handleGuildMemberMute(request *server.Request[server.GuildMemberMuteParam]) (any, error) {
	if request.Platform != "qqguild" {
		return nil, server.NotFound("guild.member.mute is not supported in current platform")
	}
	state, err := a.resolveRequestState(request.Origin, request.SelfID)
	if err != nil {
		return nil, err
	}
	api := state.apiV1
	guildID := request.Params.GuildID
	userID := request.Params.UserID

	seconds := int64(0)
	if request.Params.Duration > 0 {
		seconds = request.Params.Duration / 1000
	}
	mute := &dto.UpdateGuildMute{MuteSeconds: strconv.FormatInt(seconds, 10)}
	if err := api.MemberMute(requestContext(request.Origin), convert.SplitGuildCompositeID(guildID), userID, mute); err != nil {
		return nil, err
	}
	return map[string]any{}, nil
}

func (a *Adapter) handleGuildMemberRoleSet(request *server.Request[server.GuildMemberRoleParam]) (any, error) {
	return a.handleGuildMemberRoleChange(request, true)
}

func (a *Adapter) handleGuildMemberRoleUnset(request *server.Request[server.GuildMemberRoleParam]) (any, error) {
	return a.handleGuildMemberRoleChange(request, false)
}

func (a *Adapter) handleGuildMemberRoleChange(
	request *server.Request[server.GuildMemberRoleParam],
	set bool,
) (any, error) {
	if request.Platform != "qqguild" {
		return nil, server.NotFound("guild.member.role action is not supported in current platform")
	}
	state, err := a.resolveRequestState(request.Origin, request.SelfID)
	if err != nil {
		return nil, err
	}
	api := state.apiV1
	guildID := request.Params.GuildID
	userID := request.Params.UserID
	roleID := request.Params.RoleID

	ctx := requestContext(request.Origin)
	if set {
		if callErr := api.MemberAddRole(ctx, convert.SplitGuildCompositeID(guildID), dto.RoleID(roleID), userID, nil); callErr != nil {
			return nil, callErr
		}
	} else {
		if callErr := api.MemberDeleteRole(ctx, convert.SplitGuildCompositeID(guildID), dto.RoleID(roleID), userID, nil); callErr != nil {
			return nil, callErr
		}
	}
	return map[string]any{}, nil
}

func (a *Adapter) handleMessageCreate(request *server.Request[server.MessageCreateParam]) (any, error) {
	channelID := request.Params.ChannelID

	referrerRaw, _ := request.Params.Referrer.Get()
	referrer, err := parseMessageReferrer(referrerRaw)
	if err != nil {
		return nil, err
	}
	state, err := a.resolveRequestState(request.Origin, request.SelfID)
	if err != nil {
		return nil, err
	}
	sender := newMessageSender(state.apiV1, state.apiV2, convert.MessageFromDTO, a)
	if sender == nil {
		return []*message.Message{}, nil
	}

	result, err := sender.Send(requestContext(request.Origin), messageCreateInput{
		Platform:  request.Platform,
		ChannelID: channelID,
		Content:   request.Params.Content,
		Referrer:  referrer,
	})
	if err != nil {
		if errors.Is(err, errUnsupportedPlatform) {
			return nil, server.NotFound("unsupported platform")
		}
		return nil, err
	}
	if result == nil {
		return []*message.Message{}, nil
	}
	return result, nil
}

func (a *Adapter) handleMessageUpdate(request *server.Request[server.MessageUpdateParam]) (any, error) {
	if request.Platform != "qqguild" {
		return nil, server.NotFound("message.update is not supported in current platform")
	}
	channelID := request.Params.ChannelID
	if strings.Contains(channelID, "_") {
		return nil, server.NotFound("message.update is not supported for user-channel")
	}
	messageID := request.Params.MessageID

	state, err := a.resolveRequestState(request.Origin, request.SelfID)
	if err != nil {
		return nil, err
	}
	api := state.apiV1

	payload := &dto.MessageToCreate{Content: request.Params.Content}
	updated, err := api.PatchMessage(requestContext(request.Origin), channelID, messageID, payload)
	if err != nil {
		return nil, err
	}
	if updated == nil {
		return map[string]any{}, nil
	}
	return convert.MessageFromDTO(updated, request.Platform), nil
}

func (a *Adapter) handleMessageDelete(request *server.Request[server.MessageOpParam]) (any, error) {
	channelID := request.Params.ChannelID
	messageID := request.Params.MessageID

	state, err := a.resolveRequestState(request.Origin, request.SelfID)
	if err != nil {
		return nil, err
	}
	ctx := requestContext(request.Origin)
	var callErr error
	switch request.Platform {
	case "qqguild":
		if strings.Contains(channelID, "_") {
			callErr = state.apiV1.RetractDMMessage(ctx, convert.SplitGuildCompositeID(channelID), messageID)
		} else {
			callErr = state.apiV1.RetractMessage(ctx, channelID, messageID)
		}
	case "qq":
		if userID, direct := convert.SplitPrivateChannelID(channelID); direct {
			callErr = state.apiV2.RetractC2CMessage(ctx, userID, messageID)
		} else {
			callErr = state.apiV2.RetractGroupMessage(ctx, channelID, messageID)
		}
	default:
		return nil, server.NotFound("unsupported platform")
	}
	if callErr != nil {
		return nil, callErr
	}
	return map[string]any{}, nil
}

func (a *Adapter) handleMessageGet(request *server.Request[server.MessageOpParam]) (any, error) {
	if request.Platform != "qqguild" {
		return nil, server.NotFound("message.get is not supported in current platform")
	}
	channelID := request.Params.ChannelID
	if strings.Contains(channelID, "_") {
		return nil, server.NotFound("user-channel is not supported for message.get")
	}
	messageID := request.Params.MessageID

	state, err := a.resolveRequestState(request.Origin, request.SelfID)
	if err != nil {
		return nil, err
	}
	fetched, err := state.apiV1.Message(requestContext(request.Origin), channelID, messageID)
	if err != nil {
		return nil, err
	}
	if fetched == nil {
		return nil, server.NotFound("message not found")
	}
	return convert.MessageFromDTO(fetched, request.Platform), nil
}

func (a *Adapter) handleMessageList(request *server.Request[server.MessageListParam]) (any, error) {
	if request.Platform != "qqguild" {
		return nil, server.NotFound("message.list is not supported in current platform")
	}
	channelID := request.Params.ChannelID
	if strings.Contains(channelID, "_") {
		return nil, server.NotFound("message.list is not supported for user-channel")
	}

	state, err := a.resolveRequestState(request.Origin, request.SelfID)
	if err != nil {
		return nil, err
	}
	api := state.apiV1

	pager := &dto.MessagesPager{Limit: "20"}
	if limit, ok := request.Params.Limit.Get(); ok && limit > 0 {
		pager.Limit = strconv.FormatInt(limit, 10)
	}
	if nextValue, ok := request.Params.Next.Get(); ok {
		next := nextValue
		if next != "" {
			direction := ""
			if directionValue, ok := request.Params.Direction.Get(); ok {
				direction = strings.ToLower(string(directionValue))
			}
			switch direction {
			case "after":
				pager.Type = dto.MPTAfter
			default:
				pager.Type = dto.MPTBefore
			}
			pager.ID = next
		}
	}

	items, err := api.Messages(requestContext(request.Origin), channelID, pager)
	if err != nil {
		return nil, err
	}
	result := make([]*message.Message, 0, len(items))
	for _, item := range items {
		result = append(result, convert.MessageFromDTO(item, request.Platform))
	}

	response := &model.BidiPaginated[*message.Message]{Data: result}
	if len(result) > 0 {
		response.Prev = result[0].Id
		response.Next = result[len(result)-1].Id
	}
	return response, nil
}

func parseMessageReferrer(raw map[string]any) (messageReferrer, error) {
	result := messageReferrer{}
	if raw == nil {
		return result, nil
	}

	if msgIDRaw, ok := raw["msg_id"]; ok {
		result.MsgID = fmt.Sprint(msgIDRaw)
	}
	if directRaw, ok := raw["direct"]; ok {
		if direct, ok := asBool(directRaw); ok {
			result.Direct = direct
		}
	}
	if seqRaw, ok := raw["msg_seq"]; ok {
		parsed, ok := asInt64(seqRaw)
		if ok {
			seq, err := int64ToInt(parsed, "msg_seq")
			if err != nil {
				return result, err
			}
			result.MsgSeq = seq
			result.HasMsgSeq = true
		}
	}
	return result, nil
}

func asInt64(value any) (int64, bool) {
	switch typed := value.(type) {
	case int:
		return int64(typed), true
	case int8:
		return int64(typed), true
	case int16:
		return int64(typed), true
	case int32:
		return int64(typed), true
	case int64:
		return typed, true
	case uint:
		return int64(typed), true
	case uint8:
		return int64(typed), true
	case uint16:
		return int64(typed), true
	case uint32:
		return int64(typed), true
	case uint64:
		return int64(typed), true
	case float32:
		return int64(typed), true
	case float64:
		return int64(typed), true
	case json.Number:
		if parsed, err := typed.Int64(); err == nil {
			return parsed, true
		}
		if parsed, err := typed.Float64(); err == nil {
			return int64(parsed), true
		}
	case string:
		parsed, err := strconv.ParseInt(typed, 10, 64)
		if err == nil {
			return parsed, true
		}
	}
	return 0, false
}

func asBool(value any) (bool, bool) {
	switch typed := value.(type) {
	case bool:
		return typed, true
	case string:
		normalized := strings.ToLower(typed)
		switch normalized {
		case "true", "1", "yes", "on":
			return true, true
		case "false", "0", "no", "off":
			return false, true
		}
	case int, int8, int16, int32, int64:
		if number, ok := asInt64(typed); ok {
			return number != 0, true
		}
	case uint, uint8, uint16, uint32, uint64:
		if number, ok := asInt64(typed); ok {
			return number != 0, true
		}
	}
	return false, false
}

func (a *Adapter) handleReactionList(request *server.Request[server.ReactionListParam]) (any, error) {
	if request.Platform != "qqguild" {
		return nil, server.NotFound("reaction.list is not supported in current platform")
	}
	state, err := a.resolveRequestState(request.Origin, request.SelfID)
	if err != nil {
		return nil, err
	}
	api := state.apiV1
	channelID := request.Params.ChannelID
	messageID := request.Params.MessageID
	emojiRaw := request.Params.EmojiID

	pager := &dto.MessageReactionPager{Limit: "50"}
	if nextValue, ok := request.Params.Next.Get(); ok {
		next := nextValue
		if next != "" {
			pager.Cookie = next
		}
	}

	items, err := api.GetMessageReactionUsers(
		requestContext(request.Origin),
		convert.SplitChannelCompositeID(channelID),
		messageID,
		convert.ParseReactionEmoji(emojiRaw),
		pager,
	)
	if err != nil {
		return nil, err
	}
	if items == nil {
		return &model.Paginated[*user.User]{Data: []*user.User{}}, nil
	}

	response := &model.Paginated[*user.User]{Data: convert.ReactionUsersFromDTO(items.Users)}
	if !items.IsEnd {
		response.Next = items.Cookie
	}
	return response, nil
}

func (a *Adapter) handleReactionCreate(request *server.Request[server.ReactionCreateParam]) (any, error) {
	if request.Platform != "qqguild" {
		return nil, server.NotFound("reaction.create is not supported in current platform")
	}
	state, err := a.resolveRequestState(request.Origin, request.SelfID)
	if err != nil {
		return nil, err
	}
	api := state.apiV1
	channelID := request.Params.ChannelID
	messageID := request.Params.MessageID
	emojiRaw := request.Params.EmojiID

	if err := api.CreateMessageReaction(
		requestContext(request.Origin),
		convert.SplitChannelCompositeID(channelID),
		messageID,
		convert.ParseReactionEmoji(emojiRaw),
	); err != nil {
		return nil, err
	}
	return map[string]any{}, nil
}

func (a *Adapter) handleReactionDelete(request *server.Request[server.ReactionDeleteParam]) (any, error) {
	if request.Platform != "qqguild" {
		return nil, server.NotFound("reaction.delete is not supported in current platform")
	}
	state, err := a.resolveRequestState(request.Origin, request.SelfID)
	if err != nil {
		return nil, err
	}
	api := state.apiV1
	channelID := request.Params.ChannelID
	messageID := request.Params.MessageID
	emojiRaw := request.Params.EmojiID

	if err := api.DeleteOwnMessageReaction(
		requestContext(request.Origin),
		convert.SplitChannelCompositeID(channelID),
		messageID,
		convert.ParseReactionEmoji(emojiRaw),
	); err != nil {
		return nil, err
	}
	return map[string]any{}, nil
}

func (a *Adapter) handleReactionClear(_ *server.Request[server.ReactionClearParam]) (any, error) {
	return nil, server.NotFound("reaction.clear is not supported")
}

func (a *Adapter) registerRoutes() {
	a.RouterMixin.Route(protocol.ApiLoginGet, server.Wrapper(a.handleLoginGet))

	a.RouterMixin.Route(protocol.ApiMessageCreate, server.Wrapper(a.handleMessageCreate))
	a.RouterMixin.Route(protocol.ApiMessageUpdate, server.Wrapper(a.handleMessageUpdate))
	a.RouterMixin.Route(protocol.ApiMessageDelete, server.Wrapper(a.handleMessageDelete))
	a.RouterMixin.Route(protocol.ApiMessageGet, server.Wrapper(a.handleMessageGet))
	a.RouterMixin.Route(protocol.ApiMessageList, server.Wrapper(a.handleMessageList))

	a.RouterMixin.Route(protocol.ApiChannelGet, server.Wrapper(a.handleChannelGet))
	a.RouterMixin.Route(protocol.ApiChannelList, server.Wrapper(a.handleChannelList))
	a.RouterMixin.Route(protocol.ApiChannelCreate, server.Wrapper(a.handleChannelCreate))
	a.RouterMixin.Route(protocol.ApiChannelUpdate, server.Wrapper(a.handleChannelUpdate))
	a.RouterMixin.Route(protocol.ApiChannelDelete, server.Wrapper(a.handleChannelDelete))
	a.RouterMixin.Route(protocol.ApiChannelMute, server.Wrapper(a.handleChannelMute))

	a.RouterMixin.Route(protocol.ApiGuildGet, server.Wrapper(a.handleGuildGet))
	a.RouterMixin.Route(protocol.ApiGuildList, server.Wrapper(a.handleGuildList))
	a.RouterMixin.Route(protocol.ApiGuildApprove, unsupportedRoute("guild.approve"))

	a.RouterMixin.Route(protocol.ApiGuildMemberGet, server.Wrapper(a.handleGuildMemberGet))
	a.RouterMixin.Route(protocol.ApiGuildMemberList, server.Wrapper(a.handleGuildMemberList))
	a.RouterMixin.Route(protocol.ApiGuildMemberKick, server.Wrapper(a.handleGuildMemberKick))
	a.RouterMixin.Route(protocol.ApiGuildMemberMute, server.Wrapper(a.handleGuildMemberMute))
	a.RouterMixin.Route(protocol.ApiGuildMemberRoleSet, server.Wrapper(a.handleGuildMemberRoleSet))
	a.RouterMixin.Route(protocol.ApiGuildMemberRoleUnset, server.Wrapper(a.handleGuildMemberRoleUnset))
	a.RouterMixin.Route(protocol.ApiGuildMemberApprove, unsupportedRoute("guild.member.approve"))

	a.RouterMixin.Route(protocol.ApiGuildRoleList, server.Wrapper(a.handleGuildRoleList))
	a.RouterMixin.Route(protocol.ApiGuildRoleCreate, server.Wrapper(a.handleGuildRoleCreate))
	a.RouterMixin.Route(protocol.ApiGuildRoleUpdate, server.Wrapper(a.handleGuildRoleUpdate))
	a.RouterMixin.Route(protocol.ApiGuildRoleDelete, server.Wrapper(a.handleGuildRoleDelete))

	a.RouterMixin.Route(protocol.ApiReactionCreate, server.Wrapper(a.handleReactionCreate))
	a.RouterMixin.Route(protocol.ApiReactionDelete, server.Wrapper(a.handleReactionDelete))
	a.RouterMixin.Route(protocol.ApiReactionList, server.Wrapper(a.handleReactionList))
	a.RouterMixin.Route(protocol.ApiReactionClear, server.Wrapper(a.handleReactionClear))

	a.RouterMixin.Route(protocol.ApiUserGet, server.Wrapper(a.handleUserGet))
	a.RouterMixin.Route(protocol.ApiUserChannelCreate, server.Wrapper(a.handleUserChannelCreate))

	a.RouterMixin.Route(protocol.ApiFriendList, unsupportedRoute("friend.list"))
	a.RouterMixin.Route(protocol.ApiFriendDelete, unsupportedRoute("friend.delete"))
	a.RouterMixin.Route(protocol.ApiFriendApprove, unsupportedRoute("friend.approve"))

	a.RouterMixin.Route(protocol.ParseApi("internal/*"), server.Wrapper(a.handleInternalRoute))
}

func unsupportedRoute(action string) server.RouteCall[any, any] {
	return func(_ *server.Request[any]) (any, error) {
		return nil, server.NotFound(action + " is not supported")
	}
}

func (a *Adapter) handleGuildRoleList(request *server.Request[server.GuildListByGuildParam]) (any, error) {
	if request.Platform != "qqguild" {
		return nil, server.NotFound("guild.role.list is not supported in current platform")
	}
	state, err := a.resolveRequestState(request.Origin, request.SelfID)
	if err != nil {
		return nil, err
	}
	api := state.apiV1
	guildID := request.Params.GuildID

	roles, err := api.Roles(requestContext(request.Origin), convert.SplitGuildCompositeID(guildID))
	if err != nil {
		return nil, err
	}
	if roles == nil {
		return &model.Paginated[*guildrole.GuildRole]{Data: []*guildrole.GuildRole{}}, nil
	}
	return &model.Paginated[*guildrole.GuildRole]{Data: convert.RolesFromDTO(roles.Roles)}, nil
}

func (a *Adapter) handleGuildRoleCreate(request *server.Request[server.GuildRoleCreateParam]) (any, error) {
	if request.Platform != "qqguild" {
		return nil, server.NotFound("guild.role.create is not supported in current platform")
	}
	state, err := a.resolveRequestState(request.Origin, request.SelfID)
	if err != nil {
		return nil, err
	}
	api := state.apiV1
	guildID := request.Params.GuildID

	updated, err := api.PostRole(
		requestContext(request.Origin),
		convert.SplitGuildCompositeID(guildID),
		convert.ParseRole(request.Params.Role),
	)
	if err != nil {
		return nil, err
	}
	if updated == nil || updated.Role == nil {
		return nil, server.NotFound("role not created")
	}
	items := convert.RolesFromDTO([]*dto.Role{updated.Role})
	if len(items) == 0 {
		return nil, server.NotFound("role not created")
	}
	return items[0], nil
}

func (a *Adapter) handleGuildRoleUpdate(request *server.Request[server.GuildRoleUpdateParam]) (any, error) {
	if request.Platform != "qqguild" {
		return nil, server.NotFound("guild.role.update is not supported in current platform")
	}
	state, err := a.resolveRequestState(request.Origin, request.SelfID)
	if err != nil {
		return nil, err
	}
	api := state.apiV1
	guildID := request.Params.GuildID
	roleID := request.Params.RoleID

	updated, err := api.PatchRole(
		requestContext(request.Origin),
		convert.SplitGuildCompositeID(guildID),
		dto.RoleID(roleID),
		convert.ParseRole(request.Params.Role),
	)
	if err != nil {
		return nil, err
	}
	if updated == nil || updated.Role == nil {
		return map[string]any{}, nil
	}
	items := convert.RolesFromDTO([]*dto.Role{updated.Role})
	if len(items) == 0 {
		return map[string]any{}, nil
	}
	return items[0], nil
}

func (a *Adapter) handleGuildRoleDelete(request *server.Request[server.GuildRoleDeleteParam]) (any, error) {
	if request.Platform != "qqguild" {
		return nil, server.NotFound("guild.role.delete is not supported in current platform")
	}
	state, err := a.resolveRequestState(request.Origin, request.SelfID)
	if err != nil {
		return nil, err
	}
	api := state.apiV1
	guildID := request.Params.GuildID
	roleID := request.Params.RoleID

	if err := api.DeleteRole(requestContext(request.Origin), convert.SplitGuildCompositeID(guildID), dto.RoleID(roleID)); err != nil {
		return nil, err
	}
	return map[string]any{}, nil
}

func requestContext(request *http.Request) context.Context {
	if request == nil {
		return context.Background()
	}
	return request.Context()
}

func (a *Adapter) resolveRequestState(origin *http.Request, selfID string) (*appState, error) {
	return a.resolveStateBySelfID(requestContext(origin), selfID)
}

func int64ToInt(value int64, key string) (int, error) {
	if strconv.IntSize == 32 && (value < -2147483648 || value > 2147483647) {
		return 0, server.BadRequest(fmt.Sprintf("%s is out of range", key))
	}
	return int(value), nil
}

func (a *Adapter) handleUserChannelCreate(request *server.Request[server.UserChannelCreateParam]) (any, error) {
	userID := request.Params.UserID

	switch request.Platform {
	case "qq":
		return &channel.Channel{Id: convert.ComposePrivateChannelID(userID), Type: channel.ChannelTypeDirect}, nil
	case "qqguild":
		state, err := a.resolveRequestState(request.Origin, request.SelfID)
		if err != nil {
			return nil, err
		}
		guildIDRaw, ok := request.Params.GuildID.Get()
		if !ok {
			return nil, server.BadRequest("guild_id is required")
		}
		guildID := guildIDRaw
		dm, callErr := state.apiV1.CreateDirectMessage(requestContext(request.Origin), &dto.DirectMessageToCreate{
			RecipientID:   userID,
			SourceGuildID: guildID,
		})
		if callErr != nil {
			return nil, callErr
		}
		if dm == nil {
			return nil, server.NotFound("failed to create user channel")
		}
		baseGuildID := convert.SplitGuildCompositeID(guildID)
		return &channel.Channel{Id: convert.ComposeChannelCompositeID(dm.GuildID, baseGuildID), Type: channel.ChannelTypeText}, nil
	default:
		return nil, server.NotFound("unsupported platform")
	}
}

func (a *Adapter) handleUserGet(request *server.Request[server.UserGetParam]) (any, error) {
	if request.Platform != "qqguild" {
		return nil, server.NotFound("user.get is not supported in current platform")
	}
	userID := request.Params.UserID

	state, err := a.resolveRequestState(request.Origin, request.SelfID)
	if err != nil {
		return nil, err
	}
	api := state.apiV1

	guildID, userID := convert.SplitGuildUserCompositeID(userID)
	if guildID == "" {
		return nil, server.NotFound("qqguild platform requires user_id in guildID_userID format")
	}

	member, err := api.GuildMember(requestContext(request.Origin), convert.SplitGuildCompositeID(guildID), userID)
	if err != nil {
		return nil, err
	}
	if member == nil || member.User == nil {
		return nil, server.NotFound("user not found")
	}
	return convert.UserFromDTO(member.User), nil
}
