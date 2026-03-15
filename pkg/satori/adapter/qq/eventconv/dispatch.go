package eventconv

import (
	"context"

	botgodto "github.com/WindowsSov8forUs/botgo-plus/dto"
	qqwebhook "github.com/satori-protocol-go/satori-go/pkg/satori/adapter/qq/webhook"
	"github.com/satori-protocol-go/satori-go/pkg/satori/model/event"
	"github.com/satori-protocol-go/satori-go/pkg/satori/model/login"
)

func (c *Converter) Convert(ctx context.Context, payload *qqwebhook.Payload) (*event.Event, error) {
	if payload == nil || payload.Op != botgodto.DispatchEvent {
		return nil, nil
	}
	data, err := decodeWebhookData(payload.Data)
	if err != nil {
		return nil, err
	}

	loginValue := c.loginForEvent(ctx, string(payload.Type))
	result := c.convertDispatchEvent(ctx, payload.Type, data, loginValue)
	if result == nil {
		return nil, nil
	}
	result.Type_ = string(payload.Type)
	result.Data_ = data
	if result.Timestamp == 0 {
		result.Timestamp = currentTimestamp()
	}
	return result, nil
}

func (c *Converter) convertDispatchEvent(
	ctx context.Context,
	eventType botgodto.EventType,
	data map[string]any,
	loginValue *login.Login,
) *event.Event {
	switch eventType {
	case botgodto.EventMessageCreate, botgodto.EventAtMessageCreate:
		return c.makeGuildMessageCreatedEvent(loginValue, data)
	case botgodto.EventDirectMessageCreate:
		return c.makeGuildDirectMessageCreatedEvent(loginValue, data)
	case botgodto.EventGroupAtMessageCreate:
		return c.makeGroupMessageCreatedEvent(loginValue, data)
	case botgodto.EventC2CMessageCreate:
		return c.makeC2CMessageCreatedEvent(loginValue, data)

	case botgodto.EventMessageDelete, botgodto.EventPublicMessageDelete:
		return c.makeGuildMessageDeletedEvent(loginValue, data)
	case botgodto.EventDirectMessageDelete:
		return c.makeDirectMessageDeletedEvent(loginValue, data)

	case botgodto.EventMessageReactionAdd:
		return c.makeReactionEvent(loginValue, data, event.EventTypeReactionAdded)
	case botgodto.EventMessageReactionRemove:
		return c.makeReactionEvent(loginValue, data, event.EventTypeReactionRemoved)

	case botgodto.EventGuildCreate:
		return c.makeGuildEvent(loginValue, data, event.EventTypeGuildAdded)
	case botgodto.EventGuildUpdate:
		return c.makeGuildEvent(loginValue, data, event.EventTypeGuildUpdated)
	case botgodto.EventGuildDelete:
		return c.makeGuildEvent(loginValue, data, event.EventTypeGuildRemoved)

	case botgodto.EventChannelCreate:
		return c.makeChannelEvent(loginValue, data, eventTypeChannelAdded)
	case botgodto.EventChannelUpdate:
		return c.makeChannelEvent(loginValue, data, eventTypeChannelUpdated)
	case botgodto.EventChannelDelete:
		return c.makeChannelEvent(loginValue, data, eventTypeChannelRemoved)

	case botgodto.EventGuildMemberAdd:
		return c.makeGuildMemberEvent(loginValue, data, event.EventTypeGuildMemberAdded)
	case botgodto.EventGuildMemberUpdate:
		return c.makeGuildMemberEvent(loginValue, data, event.EventTypeGuildMemberUpdated)
	case botgodto.EventGuildMemberRemove, "GUILD_MEMBER_DELETE":
		return c.makeGuildMemberEvent(loginValue, data, event.EventTypeGuildMemberRemoved)

	case botgodto.EventGroupAddRobot:
		return c.makeGroupRobotEvent(loginValue, data, event.EventTypeGuildAdded)
	case botgodto.EventGroupDelRobot:
		return c.makeGroupRobotEvent(loginValue, data, event.EventTypeGuildRemoved)

	case botgodto.EventFriendAdd:
		return c.makeFriendEvent(loginValue, data, eventTypeFriendAdded)
	case botgodto.EventFriendDel:
		return c.makeFriendEvent(loginValue, data, eventTypeFriendRemoved)

	case botgodto.EventInteractionCreate:
		return c.makeInteractionEvent(ctx, loginValue, data)

	default:
		return &event.Event{
			Type:      event.EventTypeInternal,
			Timestamp: pickEventTimestamp(data),
			Login:     loginValue,
		}
	}
}
