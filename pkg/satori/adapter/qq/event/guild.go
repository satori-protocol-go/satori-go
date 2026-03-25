package event

import (
	"github.com/WindowsSov8forUs/botgo-plus/dto"
	satorievent "github.com/satori-protocol-go/satori-go/pkg/satori/model/event"
	"github.com/satori-protocol-go/satori-go/pkg/satori/model/guild"
	"github.com/satori-protocol-go/satori-go/pkg/satori/model/login"
	"github.com/satori-protocol-go/satori-go/pkg/satori/model/user"
)

func (c *Converter) makeGuildEvent(
	loginValue *login.Login,
	data map[string]any,
	eventType satorievent.EventType,
) *satorievent.Event {
	guildDTO := &dto.Guild{}
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
	return &satorievent.Event{
		Type:      eventType,
		Timestamp: pickEventTimestamp(data),
		Login:     loginValue,
		Guild:     guildValue,
		Operator:  operatorValue,
	}
}
