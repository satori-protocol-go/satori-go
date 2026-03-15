package action

import (
	"fmt"
	"strings"

	botgodto "github.com/WindowsSov8forUs/botgo-plus/dto"
	qqcodec "github.com/satori-protocol-go/satori-go/pkg/satori/adapter/qq/codec"
	"github.com/satori-protocol-go/satori-go/pkg/satori/model/channel"
	satoriserver "github.com/satori-protocol-go/satori-go/pkg/satori/server"
)

func (h *Handler) handleUserChannelCreate(request satoriserver.Request[satoriserver.UserChannelCreateParam]) (any, error) {
	userID, err := requiredString(request.Params.UserID, "user_id")
	if err != nil {
		return nil, err
	}

	switch request.Platform {
	case "qq":
		return &channel.Channel{Id: "private:" + userID, Type: channel.ChannelTypeDirect}, nil
	case "qqguild":
		guildIDRaw, ok := request.Params.GuildID.Get()
		if !ok {
			return nil, satoriserver.BadRequest("guild_id is required")
		}
		guildID, err := requiredString(guildIDRaw, "guild_id")
		if err != nil {
			return nil, err
		}
		dm, callErr := h.apiV1.CreateDirectMessage(requestContext(request.Origin), &botgodto.DirectMessageToCreate{
			RecipientID:   userID,
			SourceGuildID: guildID,
		})
		if callErr != nil {
			return nil, callErr
		}
		if dm == nil {
			return nil, satoriserver.NotFound("failed to create user channel")
		}
		baseGuildID := splitGuildCompositeID(guildID)
		return &channel.Channel{Id: fmt.Sprintf("%s_%s", dm.GuildID, baseGuildID), Type: channel.ChannelTypeText}, nil
	default:
		return nil, satoriserver.NotFound("unsupported platform")
	}
}

func (h *Handler) handleUserGet(request satoriserver.Request[satoriserver.UserGetParam]) (any, error) {
	if request.Platform != "qqguild" {
		return nil, satoriserver.NotFound("user.get is not supported in current platform")
	}
	userID, err := requiredString(request.Params.UserID, "user_id")
	if err != nil {
		return nil, err
	}

	api, ok := h.apiV1.(apiMemberActions)
	if !ok {
		return nil, satoriserver.NotFound("user.get capability is unavailable")
	}

	guildID := ""
	if strings.Contains(userID, "_") {
		parts := strings.SplitN(userID, "_", 2)
		guildID = parts[0]
		userID = parts[1]
	}
	if guildID == "" {
		return nil, satoriserver.NotFound("qqguild platform requires user_id in guildID_userID format")
	}

	member, err := api.GuildMember(requestContext(request.Origin), splitGuildCompositeID(guildID), userID)
	if err != nil {
		return nil, err
	}
	if member == nil || member.User == nil {
		return nil, satoriserver.NotFound("user not found")
	}
	return qqcodec.UserFromDTO(member.User), nil
}
