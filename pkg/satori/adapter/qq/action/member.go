package action

import (
	"strconv"
	"strings"

	botgodto "github.com/WindowsSov8forUs/botgo-plus/dto"
	qqcodec "github.com/satori-protocol-go/satori-go/pkg/satori/adapter/qq/codec"
	"github.com/satori-protocol-go/satori-go/pkg/satori/model/define"
	"github.com/satori-protocol-go/satori-go/pkg/satori/model/guildmember"
	satoriserver "github.com/satori-protocol-go/satori-go/pkg/satori/server"
)

func (h *Handler) handleGuildMemberGet(request satoriserver.Request[satoriserver.GuildMemberGetParam]) (any, error) {
	if request.Platform != "qqguild" {
		return nil, satoriserver.NotFound("guild.member.get is not supported in current platform")
	}
	api, ok := h.apiV1.(apiMemberActions)
	if !ok {
		return nil, satoriserver.NotFound("guild.member.get capability is unavailable")
	}
	guildID, err := requiredString(request.Params.GuildID, "guild_id")
	if err != nil {
		return nil, err
	}
	userID, err := requiredString(request.Params.UserID, "user_id")
	if err != nil {
		return nil, err
	}

	member, err := api.GuildMember(requestContext(request.Origin), splitGuildCompositeID(guildID), userID)
	if err != nil {
		return nil, err
	}
	if member == nil {
		return nil, satoriserver.NotFound("member not found")
	}
	return qqcodec.MemberFromDTO(member), nil
}

func (h *Handler) handleGuildMemberList(request satoriserver.Request[satoriserver.GuildListByGuildParam]) (any, error) {
	if request.Platform != "qqguild" {
		return nil, satoriserver.NotFound("guild.member.list is not supported in current platform")
	}
	api, ok := h.apiV1.(apiMemberActions)
	if !ok {
		return nil, satoriserver.NotFound("guild.member.list capability is unavailable")
	}
	guildID, err := requiredString(request.Params.GuildID, "guild_id")
	if err != nil {
		return nil, err
	}

	pager := &botgodto.GuildMembersPager{After: "0", Limit: "400"}
	if nextValue, ok := request.Params.Next.Get(); ok {
		next := strings.TrimSpace(nextValue)
		if next != "" {
			pager.After = next
		}
	}

	items, err := api.GuildMembers(requestContext(request.Origin), splitGuildCompositeID(guildID), pager)
	if err != nil {
		return nil, err
	}
	data := make([]*guildmember.GuildMember, 0, len(items))
	for _, item := range items {
		data = append(data, qqcodec.MemberFromDTO(item))
	}
	response := &define.Paginated[*guildmember.GuildMember]{Data: data}
	if len(data) > 0 && data[len(data)-1] != nil && data[len(data)-1].User != nil {
		response.Next = data[len(data)-1].User.Id
	}
	return response, nil
}

func (h *Handler) handleGuildMemberKick(request satoriserver.Request[satoriserver.GuildMemberKickParam]) (any, error) {
	if request.Platform != "qqguild" {
		return nil, satoriserver.NotFound("guild.member.kick is not supported in current platform")
	}
	api, ok := h.apiV1.(apiMemberActions)
	if !ok {
		return nil, satoriserver.NotFound("guild.member.kick capability is unavailable")
	}
	guildID, err := requiredString(request.Params.GuildID, "guild_id")
	if err != nil {
		return nil, err
	}
	userID, err := requiredString(request.Params.UserID, "user_id")
	if err != nil {
		return nil, err
	}

	if err := api.DeleteGuildMember(requestContext(request.Origin), splitGuildCompositeID(guildID), userID); err != nil {
		return nil, err
	}
	return map[string]any{}, nil
}

func (h *Handler) handleGuildMemberMute(request satoriserver.Request[satoriserver.GuildMemberMuteParam]) (any, error) {
	if request.Platform != "qqguild" {
		return nil, satoriserver.NotFound("guild.member.mute is not supported in current platform")
	}
	api, ok := h.apiV1.(apiMemberActions)
	if !ok {
		return nil, satoriserver.NotFound("guild.member.mute capability is unavailable")
	}
	guildID, err := requiredString(request.Params.GuildID, "guild_id")
	if err != nil {
		return nil, err
	}
	userID, err := requiredString(request.Params.UserID, "user_id")
	if err != nil {
		return nil, err
	}

	seconds := int64(0)
	if request.Params.Duration > 0 {
		seconds = request.Params.Duration / 1000
	}
	mute := &botgodto.UpdateGuildMute{MuteSeconds: strconv.FormatInt(seconds, 10)}
	if err := api.MemberMute(requestContext(request.Origin), splitGuildCompositeID(guildID), userID, mute); err != nil {
		return nil, err
	}
	return map[string]any{}, nil
}

func (h *Handler) handleGuildMemberRoleSet(request satoriserver.Request[satoriserver.GuildMemberRoleParam]) (any, error) {
	return h.handleGuildMemberRoleChange(request, true)
}

func (h *Handler) handleGuildMemberRoleUnset(request satoriserver.Request[satoriserver.GuildMemberRoleParam]) (any, error) {
	return h.handleGuildMemberRoleChange(request, false)
}

func (h *Handler) handleGuildMemberRoleChange(
	request satoriserver.Request[satoriserver.GuildMemberRoleParam],
	set bool,
) (any, error) {
	if request.Platform != "qqguild" {
		return nil, satoriserver.NotFound("guild.member.role action is not supported in current platform")
	}
	api, ok := h.apiV1.(apiMemberActions)
	if !ok {
		return nil, satoriserver.NotFound("guild.member.role capability is unavailable")
	}
	guildID, err := requiredString(request.Params.GuildID, "guild_id")
	if err != nil {
		return nil, err
	}
	userID, err := requiredString(request.Params.UserID, "user_id")
	if err != nil {
		return nil, err
	}
	roleID, err := requiredString(request.Params.RoleID, "role_id")
	if err != nil {
		return nil, err
	}

	ctx := requestContext(request.Origin)
	if set {
		err = api.MemberAddRole(ctx, splitGuildCompositeID(guildID), botgodto.RoleID(roleID), userID, nil)
	} else {
		err = api.MemberDeleteRole(ctx, splitGuildCompositeID(guildID), botgodto.RoleID(roleID), userID, nil)
	}
	if err != nil {
		return nil, err
	}
	return map[string]any{}, nil
}
