package event

import (
	"github.com/WindowsSov8forUs/botgo-plus/dto"
	"github.com/satori-protocol-go/satori-go/pkg/satori/model/channel"
	satorievent "github.com/satori-protocol-go/satori-go/pkg/satori/model/event"
	"github.com/satori-protocol-go/satori-go/pkg/satori/model/guild"
	"github.com/satori-protocol-go/satori-go/pkg/satori/model/login"
	"github.com/satori-protocol-go/satori-go/pkg/satori/model/user"
)

func (c *Converter) makeChannelEvent(
	loginValue *login.Login,
	data map[string]any,
	eventType satorievent.EventType,
) *satorievent.Event {
	channelDTO := &dto.Channel{}
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
	return &satorievent.Event{
		Type:      eventType,
		Timestamp: pickEventTimestamp(data),
		Login:     loginValue,
		Channel:   channelValue,
		Guild:     guildValue,
		Operator:  operatorValue,
	}
}
