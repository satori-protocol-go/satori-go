package event

import (
	"github.com/WindowsSov8forUs/botgo-plus/dto"
	satorievent "github.com/satori-protocol-go/satori-go/pkg/satori/model/event"
	"github.com/satori-protocol-go/satori-go/pkg/satori/model/guild"
	"github.com/satori-protocol-go/satori-go/pkg/satori/model/guildmember"
	"github.com/satori-protocol-go/satori-go/pkg/satori/model/guildrole"
	"github.com/satori-protocol-go/satori-go/pkg/satori/model/login"
	"github.com/satori-protocol-go/satori-go/pkg/satori/model/user"
)

func (c *Converter) makeGuildMemberEvent(
	loginValue *login.Login,
	data map[string]any,
	eventType satorievent.EventType,
) *satorievent.Event {
	memberDTO := &dto.Member{}
	_ = decodeInto(data, memberDTO)

	memberValue := c.memberFromDTO(memberDTO)
	if memberValue == nil {
		memberValue = &guildmember.GuildMember{
			User: &user.User{Id: valueAsString(data["user_id"])},
			Nick: valueAsString(data["nick"]),
		}
	}
	guildID := firstNonEmpty(memberDTO.GuildID, valueAsString(data["guild_id"]))
	guildValue := &guild.Guild{Id: guildID}
	if guildID == "" {
		guildValue = nil
	}
	operatorID := firstNonEmpty(memberDTO.OpUserID, valueAsString(data["op_user_id"]))
	operatorValue := &user.User{Id: operatorID}
	if operatorID == "" {
		operatorValue = nil
	}
	var roleValue *guildrole.GuildRole
	if len(memberDTO.Roles) > 0 && memberDTO.Roles[0] != "" {
		roleValue = &guildrole.GuildRole{Id: memberDTO.Roles[0]}
	}
	return &satorievent.Event{
		Type:      eventType,
		Timestamp: pickEventTimestamp(data),
		Login:     loginValue,
		Guild:     guildValue,
		User:      memberValue.User,
		Member:    memberValue,
		Operator:  operatorValue,
		Role:      roleValue,
	}
}
