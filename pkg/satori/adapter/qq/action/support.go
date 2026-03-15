package action

import (
	"context"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	botgodto "github.com/WindowsSov8forUs/botgo-plus/dto"
	satoriserver "github.com/satori-protocol-go/satori-go/pkg/satori/server"
)

type apiMessageAdvanced interface {
	Messages(ctx context.Context, channelID string, pager *botgodto.MessagesPager) ([]*botgodto.Message, error)
	PatchMessage(ctx context.Context, channelID string, messageID string, msg *botgodto.MessageToCreate) (*botgodto.Message, error)
}

type apiMessageMultipart interface {
	PostMessageMultipart(
		ctx context.Context,
		channelID string,
		msg *botgodto.MessageToCreate,
		fileImageData []byte,
	) (*botgodto.Message, error)
	PostDirectMessageMultipart(
		ctx context.Context,
		dm *botgodto.DirectMessage,
		msg *botgodto.MessageToCreate,
		fileImageData []byte,
	) (*botgodto.Message, error)
}

type apiChannelActions interface {
	Channel(ctx context.Context, channelID string) (*botgodto.Channel, error)
	Channels(ctx context.Context, guildID string) ([]*botgodto.Channel, error)
	PostChannel(ctx context.Context, guildID string, value *botgodto.ChannelValueObject) (*botgodto.Channel, error)
	PatchChannel(ctx context.Context, channelID string, value *botgodto.ChannelValueObject) (*botgodto.Channel, error)
	DeleteChannel(ctx context.Context, channelID string) error
}

type apiGuildActions interface {
	Guild(ctx context.Context, guildID string) (*botgodto.Guild, error)
	MeGuilds(ctx context.Context, pager *botgodto.GuildPager) ([]*botgodto.Guild, error)
}

type apiMemberActions interface {
	GuildMember(ctx context.Context, guildID string, userID string) (*botgodto.Member, error)
	GuildMembers(ctx context.Context, guildID string, pager *botgodto.GuildMembersPager) ([]*botgodto.Member, error)
	DeleteGuildMember(ctx context.Context, guildID string, userID string, opts ...botgodto.MemberDeleteOption) error
	MemberMute(ctx context.Context, guildID string, userID string, mute *botgodto.UpdateGuildMute) error
	MemberAddRole(
		ctx context.Context,
		guildID string,
		roleID botgodto.RoleID,
		userID string,
		value *botgodto.MemberAddRoleBody,
	) error
	MemberDeleteRole(
		ctx context.Context,
		guildID string,
		roleID botgodto.RoleID,
		userID string,
		value *botgodto.MemberAddRoleBody,
	) error
}

type apiRoleActions interface {
	Roles(ctx context.Context, guildID string) (*botgodto.GuildRoles, error)
	PostRole(ctx context.Context, guildID string, role *botgodto.Role) (*botgodto.UpdateResult, error)
	PatchRole(
		ctx context.Context,
		guildID string,
		roleID botgodto.RoleID,
		role *botgodto.Role,
	) (*botgodto.UpdateResult, error)
	DeleteRole(ctx context.Context, guildID string, roleID botgodto.RoleID) error
}

type apiReactionActions interface {
	CreateMessageReaction(ctx context.Context, channelID string, messageID string, emoji botgodto.Emoji) error
	DeleteOwnMessageReaction(ctx context.Context, channelID string, messageID string, emoji botgodto.Emoji) error
	GetMessageReactionUsers(
		ctx context.Context,
		channelID string,
		messageID string,
		emoji botgodto.Emoji,
		pager *botgodto.MessageReactionPager,
	) (*botgodto.MessageReactionUsers, error)
}

func requestContext(request *http.Request) context.Context {
	if request == nil {
		return context.Background()
	}
	return request.Context()
}

func requiredString(text string, key string) (string, error) {
	text = strings.TrimSpace(text)
	if text == "" {
		return "", satoriserver.BadRequest(fmt.Sprintf("%s is required", key))
	}
	return text, nil
}

func optionalString(text string) string {
	return strings.TrimSpace(text)
}

func int64ToInt(value int64, key string) (int, error) {
	if strconv.IntSize == 32 && (value < -2147483648 || value > 2147483647) {
		return 0, satoriserver.BadRequest(fmt.Sprintf("%s is out of range", key))
	}
	return int(value), nil
}

func splitGuildCompositeID(id string) string {
	if strings.Contains(id, "_") {
		return strings.SplitN(id, "_", 2)[0]
	}
	return id
}

func splitChannelCompositeID(id string) string {
	if strings.Contains(id, "_") {
		parts := strings.Split(id, "_")
		return parts[len(parts)-1]
	}
	return id
}
