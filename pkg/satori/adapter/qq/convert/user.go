package convert

import (
	"github.com/WindowsSov8forUs/botgo-plus/dto"
	"github.com/satori-protocol-go/satori-go/pkg/satori/model/guildmember"
	"github.com/satori-protocol-go/satori-go/pkg/satori/model/user"
)

func UserFromDTO(input *dto.User) *user.User {
	if input == nil {
		return nil
	}
	id := firstNonEmpty(input.ID, input.MemberOpenID, input.UserOpenID, input.UnionOpenID)
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
	return result
}

func ReactionUsersFromDTO(items []*dto.User) []*user.User {
	result := make([]*user.User, 0, len(items))
	for _, item := range items {
		result = append(result, UserFromDTO(item))
	}
	return result
}
