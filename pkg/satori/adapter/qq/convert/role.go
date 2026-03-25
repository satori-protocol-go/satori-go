package convert

import (
	"strings"

	"github.com/WindowsSov8forUs/botgo-plus/dto"
	"github.com/satori-protocol-go/satori-go/pkg/satori/model"
	"github.com/satori-protocol-go/satori-go/pkg/satori/model/guild"
	"github.com/satori-protocol-go/satori-go/pkg/satori/model/guildrole"
)

func GuildFromDTO(input *dto.Guild) *guild.Guild {
	if input == nil {
		return nil
	}
	return &guild.Guild{Id: input.ID, Name: input.Name, Avatar: input.Icon}
}

func RolesFromDTO(items []*dto.Role) []*guildrole.GuildRole {
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

func ParseRole(raw model.GuildRole) *dto.Role {
	role := &dto.Role{}
	role.Name = strings.TrimSpace(raw.Name)
	return role
}
