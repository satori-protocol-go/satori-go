package event

import (
	"encoding/json"
	"errors"
	"fmt"

	"github.com/WindowsSov8forUs/botgo-plus/dto"
)

// Validate known event shapes before the legacy conversion helpers can discard decoding errors.
// Unknown events remain internal and retain their native data. Optional fields stay optional.
func validateDispatchData(typ dto.EventType, data map[string]any) error {
	var target any
	switch typ {
	case dto.EventMessageCreate, dto.EventAtMessageCreate, dto.EventDirectMessageCreate, dto.EventGroupAtMessageCreate, dto.EventGroupMessageCreate, dto.EventC2CMessageCreate:
		target = &dto.Message{}
	case dto.EventMessageDelete, dto.EventPublicMessageDelete, dto.EventDirectMessageDelete:
		target = &struct {
			Message  *dto.Message `json:"message"`
			Operator *dto.User    `json:"op_user"`
		}{}
	case dto.EventMessageReactionAdd, dto.EventMessageReactionRemove:
		target = &dto.MessageReaction{}
	case dto.EventGuildCreate, dto.EventGuildUpdate, dto.EventGuildDelete:
		target = &dto.Guild{}
	case dto.EventChannelCreate, dto.EventChannelUpdate, dto.EventChannelDelete:
		target = &dto.Channel{}
	case dto.EventGuildMemberAdd, dto.EventGuildMemberUpdate, dto.EventGuildMemberRemove, "GUILD_MEMBER_DELETE":
		target = &dto.Member{}
	case dto.EventGroupAddRobot, dto.EventGroupDelRobot:
		target = &dto.GroupRobotEvent{}
	case dto.EventGroupMemberAdd, dto.EventGroupMemberRemove, dto.EventGroupJoinRequest, dto.EventC2CFriendAdd, dto.EventC2CFriendDel:
		// These converters read scalar fields directly and intentionally accept numeric IDs.
		return nil
	case dto.EventInteractionCreate:
		target = &dto.Interaction{}
	default:
		return nil
	}
	if data == nil {
		return fmt.Errorf("convert QQ event %s: expected an object", typ)
	}
	raw, err := json.Marshal(data)
	if err == nil {
		err = json.Unmarshal(raw, target)
	}
	if err != nil {
		return fmt.Errorf("convert QQ event %s: %w", typ, err)
	}
	if msg, ok := target.(*dto.Message); ok && msg.ID == "" {
		return fmt.Errorf("convert QQ event %s: message ID is missing", typ)
	}
	if interaction, ok := target.(*dto.Interaction); ok && interaction.Data != nil && len(interaction.Data.Resolved) > 0 {
		var resolved struct {
			UserID       string           `json:"user_id"`
			ButtonID     string           `json:"button_id"`
			ButtonData   string           `json:"button_data"`
			FeatureID    string           `json:"feature_id"`
			MessageScene dto.MessageScene `json:"message_scene"`
		}
		if err := json.Unmarshal(interaction.Data.Resolved, &resolved); err != nil {
			return fmt.Errorf("convert QQ interaction resolved data: %w", err)
		}
	}
	if typ == dto.EventMessageDelete || typ == dto.EventPublicMessageDelete || typ == dto.EventDirectMessageDelete {
		message, ok := data["message"].(map[string]any)
		if !ok || valueAsString(message["id"]) == "" {
			return errors.New("convert QQ deletion event: message ID is missing")
		}
	}
	return nil
}
