package event

import (
	"context"
	"encoding/json"

	"github.com/WindowsSov8forUs/botgo-plus/dto"
	satorievent "github.com/satori-protocol-go/satori-go/pkg/satori/model/event"
	"github.com/satori-protocol-go/satori-go/pkg/satori/model/login"
)

func (c *Converter) Convert(
	ctx context.Context,
	op dto.OPCode,
	eventType dto.EventType,
	rawData json.RawMessage,
) (*satorievent.Event, error) {
	if op != dto.DispatchEvent {
		return nil, nil
	}
	data, err := decodeWebhookData(rawData)
	if err != nil {
		return nil, err
	}

	loginValue := c.loginForEvent(ctx, string(eventType))
	result := c.convertDispatchEvent(ctx, eventType, data, loginValue)
	if result == nil {
		return nil, nil
	}
	result.Type_ = string(eventType)
	result.Data_ = data
	if result.Timestamp == 0 {
		result.Timestamp = currentTimestamp()
	}
	return result, nil
}

func (c *Converter) convertDispatchEvent(
	ctx context.Context,
	eventType dto.EventType,
	data map[string]any,
	loginValue *login.Login,
) *satorievent.Event {
	switch eventType {
	case dto.EventMessageCreate, dto.EventAtMessageCreate:
		return c.makeGuildMessageCreatedEvent(loginValue, data)
	case dto.EventDirectMessageCreate:
		return c.makeGuildDirectMessageCreatedEvent(loginValue, data)
	case dto.EventGroupAtMessageCreate:
		return c.makeGroupMessageCreatedEvent(loginValue, data)
	case dto.EventC2CMessageCreate:
		return c.makeC2CMessageCreatedEvent(loginValue, data)

	case dto.EventMessageDelete, dto.EventPublicMessageDelete:
		return c.makeGuildMessageDeletedEvent(loginValue, data)
	case dto.EventDirectMessageDelete:
		return c.makeDirectMessageDeletedEvent(loginValue, data)

	case dto.EventMessageReactionAdd:
		return c.makeReactionEvent(loginValue, data, satorievent.EventTypeReactionAdded)
	case dto.EventMessageReactionRemove:
		return c.makeReactionEvent(loginValue, data, satorievent.EventTypeReactionRemoved)

	case dto.EventGuildCreate:
		return c.makeGuildEvent(loginValue, data, satorievent.EventTypeGuildAdded)
	case dto.EventGuildUpdate:
		return c.makeGuildEvent(loginValue, data, satorievent.EventTypeGuildUpdated)
	case dto.EventGuildDelete:
		return c.makeGuildEvent(loginValue, data, satorievent.EventTypeGuildRemoved)

	case dto.EventChannelCreate:
		return c.makeChannelEvent(loginValue, data, eventTypeChannelAdded)
	case dto.EventChannelUpdate:
		return c.makeChannelEvent(loginValue, data, eventTypeChannelUpdated)
	case dto.EventChannelDelete:
		return c.makeChannelEvent(loginValue, data, eventTypeChannelRemoved)

	case dto.EventGuildMemberAdd:
		return c.makeGuildMemberEvent(loginValue, data, satorievent.EventTypeGuildMemberAdded)
	case dto.EventGuildMemberUpdate:
		return c.makeGuildMemberEvent(loginValue, data, satorievent.EventTypeGuildMemberUpdated)
	case dto.EventGuildMemberRemove, "GUILD_MEMBER_DELETE":
		return c.makeGuildMemberEvent(loginValue, data, satorievent.EventTypeGuildMemberRemoved)

	case dto.EventGroupAddRobot:
		return c.makeGroupRobotEvent(loginValue, data, satorievent.EventTypeGuildAdded)
	case dto.EventGroupDelRobot:
		return c.makeGroupRobotEvent(loginValue, data, satorievent.EventTypeGuildRemoved)

	case dto.EventFriendAdd:
		return c.makeFriendEvent(loginValue, data, eventTypeFriendAdded)
	case dto.EventFriendDel:
		return c.makeFriendEvent(loginValue, data, eventTypeFriendRemoved)

	case dto.EventInteractionCreate:
		return c.makeInteractionEvent(ctx, loginValue, data)

	default:
		return &satorievent.Event{
			Type:      satorievent.EventTypeInternal,
			Timestamp: pickEventTimestamp(data),
			Login:     loginValue,
		}
	}
}
