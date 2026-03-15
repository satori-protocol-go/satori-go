package codec

import (
	"fmt"

	botgodto "github.com/WindowsSov8forUs/botgo-plus/dto"
	"github.com/satori-protocol-go/satori-go/pkg/satori/model/channel"
	"github.com/satori-protocol-go/satori-go/pkg/satori/model/emoji"
	"github.com/satori-protocol-go/satori-go/pkg/satori/model/guild"
	"github.com/satori-protocol-go/satori-go/pkg/satori/model/guildmember"
	"github.com/satori-protocol-go/satori-go/pkg/satori/model/guildrole"
	"github.com/satori-protocol-go/satori-go/pkg/satori/model/user"
)

func RolesFromDTO(items []*botgodto.Role) []*guildrole.GuildRole {
	result := make([]*guildrole.GuildRole, 0, len(items))
	for _, item := range items {
		if item == nil {
			continue
		}
		result = append(result, &guildrole.GuildRole{
			Id:   string(item.ID),
			Name: item.Name,
		})
	}
	return result
}

func ReactionUsersFromDTO(items []*botgodto.User) []*user.User {
	result := make([]*user.User, 0, len(items))
	for _, item := range items {
		result = append(result, UserFromDTO(item))
	}
	return result
}

func ToSatoriEmoji(item botgodto.Emoji) *emoji.Emoji {
	if item.ID == "" {
		return nil
	}
	name := item.ID
	if item.Type != 1 {
		name = fmt.Sprintf("%d:%s", item.Type, item.ID)
	}
	return &emoji.Emoji{Id: item.ID, Name: name}
}

func UserFromDTO(input *botgodto.User) *user.User {
	if input == nil {
		return nil
	}
	id := firstNonEmpty(input.ID, input.MemberOpenID, input.UserOpenID, input.UnionOpenID)
	return &user.User{Id: id, Name: input.Username, Avatar: input.Avatar, IsBot: input.Bot}
}

func MemberFromDTO(input *botgodto.Member) *guildmember.GuildMember {
	if input == nil {
		return nil
	}
	result := &guildmember.GuildMember{Nick: input.Nick, User: UserFromDTO(input.User)}
	if result.User != nil {
		result.Avatar = result.User.Avatar
	}
	if joinedAt, err := input.JoinedAt.Time(); err == nil {
		result.JoinedAt = joinedAt.UnixMilli()
	}
	return result
}

func GuildFromDTO(input *botgodto.Guild) *guild.Guild {
	if input == nil {
		return nil
	}
	return &guild.Guild{Id: input.ID, Name: input.Name, Avatar: input.Icon}
}

func ChannelFromDTO(input *botgodto.Channel) *channel.Channel {
	if input == nil {
		return nil
	}
	return &channel.Channel{
		Id:       input.ID,
		Type:     channelTypeFromDTO(input.Type),
		Name:     input.Name,
		ParentId: input.ParentID,
	}
}

func channelTypeFromDTO(raw botgodto.ChannelType) channel.ChannelType {
	switch raw {
	case botgodto.ChannelTypeVoice:
		return channel.ChannelTypeVoice
	case botgodto.ChannelTypeCategory:
		return channel.ChannelTypeCategory
	default:
		return channel.ChannelTypeText
	}
}
