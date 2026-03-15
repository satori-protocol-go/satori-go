package eventconv

import (
	botgodto "github.com/WindowsSov8forUs/botgo-plus/dto"
	"github.com/satori-protocol-go/satori-go/pkg/satori/model/event"
	"github.com/satori-protocol-go/satori-go/pkg/satori/model/guild"
	"github.com/satori-protocol-go/satori-go/pkg/satori/model/login"
	"github.com/satori-protocol-go/satori-go/pkg/satori/model/user"
)

func (c *Converter) makeGroupRobotEvent(
	loginValue *login.Login,
	data map[string]any,
	eventType event.EventType,
) *event.Event {
	groupEvent := &botgodto.GroupAddBotEvent{}
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
	return &event.Event{
		Type:      eventType,
		Timestamp: pickEventTimestamp(data),
		Login:     loginValue,
		Guild:     guildValue,
		Operator:  operatorValue,
	}
}

func (c *Converter) makeFriendEvent(
	loginValue *login.Login,
	data map[string]any,
	eventType event.EventType,
) *event.Event {
	userID := firstNonEmpty(valueAsString(data["openid"]), valueAsString(data["user_openid"]))
	userValue := &user.User{Id: userID}
	if userID == "" {
		userValue = nil
	}
	return &event.Event{
		Type:      eventType,
		Timestamp: pickEventTimestamp(data),
		Login:     loginValue,
		User:      userValue,
	}
}
