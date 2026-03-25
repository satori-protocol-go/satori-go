package event

import (
	"github.com/WindowsSov8forUs/botgo-plus/dto"
	satorievent "github.com/satori-protocol-go/satori-go/pkg/satori/model/event"
	"github.com/satori-protocol-go/satori-go/pkg/satori/model/guild"
	"github.com/satori-protocol-go/satori-go/pkg/satori/model/login"
	"github.com/satori-protocol-go/satori-go/pkg/satori/model/user"
)

func (c *Converter) makeGroupRobotEvent(
	loginValue *login.Login,
	data map[string]any,
	eventType satorievent.EventType,
) *satorievent.Event {
	groupEvent := &dto.GroupAddBotEvent{}
	_ = decodeInto(data, groupEvent)
	guildID := firstNonEmpty(groupEvent.GroupOpenID, valueAsString(data["group_openid"]), valueAsString(data["guild_id"]))
	operatorID := firstNonEmpty(groupEvent.OpMemberOpenID, valueAsString(data["op_member_openid"]))
	operatorValue := &user.User{Id: operatorID}
	if operatorID == "" {
		operatorValue = nil
	}
	var guildValue *guild.Guild
	if guildID != "" {
		guildValue = &guild.Guild{Id: guildID}
	}
	return &satorievent.Event{
		Type:      eventType,
		Timestamp: pickEventTimestamp(data),
		Login:     loginValue,
		Guild:     guildValue,
		Operator:  operatorValue,
	}
}
