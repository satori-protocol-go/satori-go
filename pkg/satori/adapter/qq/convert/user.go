package convert

import (
	"errors"
	"github.com/WindowsSov8forUs/botgo-plus/dto"
	"github.com/satori-protocol-go/satori-go/pkg/satori/model/guildmember"
	"github.com/satori-protocol-go/satori-go/pkg/satori/model/guildrole"
	"github.com/satori-protocol-go/satori-go/pkg/satori/model/user"
	"time"
)

func UserFromDTO(input *dto.User) *user.User {
	if input == nil {
		return nil
	}
	id := input.EffectiveUserID()
	return &user.User{Id: id, Name: input.Username, Avatar: input.Avatar, IsBot: input.Bot}
}

func MemberFromDTO(input *dto.Member) *guildmember.GuildMember {
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
	for _, role := range input.Roles {
		if role != "" {
			result.Roles = append(result.Roles, &guildrole.GuildRole{Id: role})
		}
	}
	return result
}

func GroupMemberFromNative(input *dto.QQGroupMember) (*guildmember.GuildMember, error) {
	if input == nil || input.MemberOpenID == "" {
		return nil, errors.New("QQ group member response has no member_openid")
	}
	result := &guildmember.GuildMember{User: &user.User{Id: input.MemberOpenID, Name: input.Username, IsBot: input.Bot}}
	if input.MemberRole != "" {
		result.Roles = []*guildrole.GuildRole{{Id: input.MemberRole}}
	}
	if input.JoinedAt != "" {
		joined, err := time.Parse(time.RFC3339Nano, input.JoinedAt)
		if err != nil {
			return nil, err
		}
		result.JoinedAt = joined.UnixMilli()
	}
	return result, nil
}

func ReactionUsersFromDTO(items []*dto.User) []*user.User {
	result := make([]*user.User, 0, len(items))
	for _, item := range items {
		result = append(result, UserFromDTO(item))
	}
	return result
}
