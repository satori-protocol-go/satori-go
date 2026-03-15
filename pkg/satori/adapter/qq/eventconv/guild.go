package eventconv

import (
	botgodto "github.com/WindowsSov8forUs/botgo-plus/dto"
	"github.com/satori-protocol-go/satori-go/pkg/satori/model/channel"
	"github.com/satori-protocol-go/satori-go/pkg/satori/model/event"
	"github.com/satori-protocol-go/satori-go/pkg/satori/model/guild"
	"github.com/satori-protocol-go/satori-go/pkg/satori/model/guildmember"
	"github.com/satori-protocol-go/satori-go/pkg/satori/model/guildrole"
	"github.com/satori-protocol-go/satori-go/pkg/satori/model/login"
	"github.com/satori-protocol-go/satori-go/pkg/satori/model/user"
)

func (c *Converter) makeGuildEvent(
	loginValue *login.Login,
	data map[string]any,
	eventType event.EventType,
) *event.Event {
	guildDTO := &botgodto.Guild{}
	_ = decodeInto(data, guildDTO)
	guildValue := c.guildFromDTO(guildDTO)
	if guildValue == nil {
		guildValue = &guild.Guild{Id: valueAsString(data["id"])}
	}
	operatorID := firstNonEmpty(guildDTO.OpUserID, valueAsString(data["op_user_id"]))
	operatorValue := &user.User{Id: operatorID}
	if operatorValue.Id == "" {
		operatorValue = nil
	}
	return &event.Event{
		Type:      eventType,
		Timestamp: pickEventTimestamp(data),
		Login:     loginValue,
		Guild:     guildValue,
		Operator:  operatorValue,
	}
}

func (c *Converter) makeChannelEvent(
	loginValue *login.Login,
	data map[string]any,
	eventType event.EventType,
) *event.Event {
	channelDTO := &botgodto.Channel{}
	_ = decodeInto(data, channelDTO)
	channelValue := c.channelFromDTO(channelDTO)
	if channelValue == nil {
		channelValue = &channel.Channel{
			Id:       valueAsString(data["id"]),
			Name:     valueAsString(data["name"]),
			ParentId: valueAsString(data["parent_id"]),
		}
	}

	guildID := firstNonEmpty(channelDTO.GuildID, valueAsString(data["guild_id"]))
	guildValue := &guild.Guild{Id: guildID}
	if guildID == "" {
		guildValue = nil
	}
	operatorID := firstNonEmpty(channelDTO.OpUserID, valueAsString(data["op_user_id"]))
	operatorValue := &user.User{Id: operatorID}
	if operatorID == "" {
		operatorValue = nil
	}
	return &event.Event{
		Type:      eventType,
		Timestamp: pickEventTimestamp(data),
		Login:     loginValue,
		Channel:   channelValue,
		Guild:     guildValue,
		Operator:  operatorValue,
	}
}

func (c *Converter) makeGuildMemberEvent(
	loginValue *login.Login,
	data map[string]any,
	eventType event.EventType,
) *event.Event {
	memberDTO := &botgodto.Member{}
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
	return &event.Event{
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
