package qq

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/WindowsSov8forUs/botgo-plus/dto"
	"github.com/satori-protocol-go/satori-go/pkg/satori/logging"
	"github.com/satori-protocol-go/satori-go/pkg/satori/model/event"
)

// Describe accepted events without logging message bodies, reply credentials or native envelopes.
func (a *Adapter) logEventBySource(ctx context.Context, typ dto.EventType, evt *event.Event) {
	if evt == nil {
		return
	}
	platform, selfID, channelID, messageID, userID, guildID, operatorID := "", "", "", "", "", "", ""
	if evt.Login != nil {
		platform = evt.Login.Platform
		if evt.Login.User != nil {
			selfID = evt.Login.User.Id
		}
	}
	if evt.Channel != nil {
		channelID = evt.Channel.Id
	}
	if evt.Guild != nil {
		guildID = evt.Guild.Id
	}
	if evt.User != nil {
		userID = evt.User.Id
	}
	if evt.Operator != nil {
		operatorID = evt.Operator.Id
	}
	if evt.Message != nil {
		messageID = evt.Message.Id
		if userID == "" && evt.Message.User != nil {
			userID = evt.Message.User.Id
		}
		if channelID == "" && evt.Message.Channel != nil {
			channelID = evt.Message.Channel.Id
		}
		if guildID == "" && evt.Message.Guild != nil {
			guildID = evt.Message.Guild.Id
		}
	}
	bot, user, guild, channel, message := eventLogID(selfID), eventLogID(userID), eventLogID(guildID), eventLogID(channelID), eventLogID(messageID)
	text := ""
	switch string(typ) {
	case "GROUP_MESSAGE_CREATE", "GROUP_AT_MESSAGE_CREATE":
		kind := "message"
		if typ == "GROUP_AT_MESSAGE_CREATE" {
			kind = "mention"
		}
		group := guild
		if guildID == "" {
			group = channel
		}
		text = fmt.Sprintf("Received QQ group %s %s from user %s in group %s for bot %s.", kind, message, user, group, bot)
	case "C2C_MESSAGE_CREATE":
		text = fmt.Sprintf("Received private QQ message %s from user %s for bot %s.", message, user, bot)
	case "MESSAGE_CREATE", "AT_MESSAGE_CREATE":
		text = fmt.Sprintf("Received message %s from user %s in guild %s, channel %s, for bot %s.", message, user, guild, channel, bot)
	case "DIRECT_MESSAGE_CREATE":
		text = fmt.Sprintf("Received guild direct message %s from user %s for bot %s.", message, user, bot)
	case "GROUP_ADD_ROBOT":
		text = fmt.Sprintf("Bot %s joined QQ group %s.", bot, guild)
	case "GROUP_DEL_ROBOT":
		text = fmt.Sprintf("Bot %s is no longer in QQ group %s.", bot, guild)
	case "GUILD_CREATE":
		text = fmt.Sprintf("QQ reported guild %s as available to bot %s.", guild, bot)
	case "GUILD_UPDATE":
		text = fmt.Sprintf("QQ reported updated information for guild %s.", guild)
	case "GUILD_DELETE":
		text = fmt.Sprintf("QQ reported guild %s as unavailable to bot %s.", guild, bot)
	case "CHANNEL_CREATE", "CHANNEL_UPDATE", "CHANNEL_DELETE":
		action := map[dto.EventType]string{"CHANNEL_CREATE": "created", "CHANNEL_UPDATE": "updated", "CHANNEL_DELETE": "deleted"}[typ]
		text = fmt.Sprintf("Channel %s was %s in guild %s.", channel, action, guild)
	case "GUILD_MEMBER_ADD", "GROUP_MEMBER_ADD":
		place := "guild"
		if typ == "GROUP_MEMBER_ADD" {
			place = "QQ group"
		}
		text = fmt.Sprintf("User %s joined %s %s.", user, place, guild)
	case "GUILD_MEMBER_REMOVE", "GUILD_MEMBER_DELETE", "GROUP_MEMBER_REMOVE":
		place := "guild"
		if typ == "GROUP_MEMBER_REMOVE" {
			place = "QQ group"
		}
		text = fmt.Sprintf("User %s left %s %s.", user, place, guild)
		if operatorID != "" && userID != "" && operatorID != userID {
			text = fmt.Sprintf("User %s was removed from %s %s by user %s.", user, place, guild, eventLogID(operatorID))
		}
	case "GUILD_MEMBER_UPDATE":
		text = fmt.Sprintf("Information for user %s in guild %s was updated.", user, guild)
	case "GROUP_JOIN_REQUEST":
		text = fmt.Sprintf("User %s requested to join QQ group %s.", user, guild)
	case "C2C_FRIEND_ADD":
		text = fmt.Sprintf("User %s added bot %s as a friend.", user, bot)
	case "C2C_FRIEND_DEL":
		text = fmt.Sprintf("User %s removed bot %s from their friends.", user, bot)
	case "MESSAGE_DELETE", "PUBLIC_MESSAGE_DELETE", "DIRECT_MESSAGE_DELETE":
		text = fmt.Sprintf("Message %s was removed from channel %s.", message, channel)
		if operatorID != "" {
			text = fmt.Sprintf("User %s recalled message %s in channel %s.", eventLogID(operatorID), message, channel)
		}
	case "MESSAGE_REACTION_ADD", "MESSAGE_REACTION_REMOVE":
		fields := eventLogFields(evt.Data_)
		action := "added"
		if typ == "MESSAGE_REACTION_REMOVE" {
			action = "removed"
		}
		target := "item " + message
		if fields.Target.Type == 0 && messageID != "" {
			target = "message " + message
		}
		text = fmt.Sprintf("User %s %s reaction %s on %s in channel %s.", user, action, eventLogID(fields.Emoji.ID), target, channel)
	case "INTERACTION_CREATE":
		text = fmt.Sprintf("Received an interaction from user %s.", user)
		if evt.Button != nil {
			text = fmt.Sprintf("User %s clicked button %s.", user, eventLogID(evt.Button.Id))
		} else if evt.Argv != nil {
			text = fmt.Sprintf("User %s invoked command %s.", user, eventLogID(evt.Argv.Name))
		}
	case "GROUP_MSG_REJECT", "GROUP_MSG_RECEIVE", "C2C_MSG_REJECT", "C2C_MSG_RECEIVE":
		fields := eventLogFields(evt.Data_)
		action := "stopped"
		if typ == "GROUP_MSG_RECEIVE" || typ == "C2C_MSG_RECEIVE" {
			action = "resumed"
		}
		if typ == "GROUP_MSG_REJECT" || typ == "GROUP_MSG_RECEIVE" {
			if fields.GroupID != "" {
				text = fmt.Sprintf("QQ group %s %s receiving bot messages.", eventLogID(fields.GroupID), action)
			}
		} else if fields.UserID != "" {
			text = fmt.Sprintf("User %s %s receiving bot messages.", eventLogID(fields.UserID), action)
		}
	case "MESSAGE_AUDIT_PASS", "MESSAGE_AUDIT_REJECT":
		// captureAuditResult records the outcome; this call describes event delivery only.
		text = fmt.Sprintf("Queued QQ audit event %s for %s bot %s.", typ, eventLogID(platform), bot)
	}
	if text == "" {
		text = "Received QQ event " + eventLogID(string(typ))
		if selfID != "" {
			if platform != "" {
				text += " for " + eventLogID(platform) + " bot " + bot
			} else {
				text += " for bot " + bot
			}
		}
		if appID := appIDFromContext(ctx); appID != "" {
			text += " (app " + eventLogID(appID) + ")"
		}
		text += "."
	}
	a.log(ctx, logging.LevelInfo, text)
}

func eventLogID(value string) string {
	if value == "" {
		return "unknown"
	}
	return logging.SafeText(value)
}

type eventLogDetails struct {
	GroupID string `json:"group_openid"`
	UserID  string `json:"user_openid"`
	OpenID  string `json:"openid"`
	Emoji   struct {
		ID string `json:"id"`
	} `json:"emoji"`
	Target struct {
		Type int `json:"type"`
	} `json:"target"`
}

// Only decode fields absent from the converted event. Never render the native envelope itself.
func eventLogFields(value any) eventLogDetails {
	var raw []byte
	switch data := value.(type) {
	case json.RawMessage:
		raw = data
	case []byte:
		raw = data
	case map[string]any:
		if body, ok := data["d"].(map[string]any); ok {
			data = body
		}
		selected := map[string]any{"group_openid": data["group_openid"], "user_openid": data["user_openid"], "openid": data["openid"], "emoji": data["emoji"], "target": data["target"]}
		raw, _ = json.Marshal(selected)
	}
	var envelope struct {
		Data json.RawMessage `json:"d"`
	}
	if json.Unmarshal(raw, &envelope) == nil && len(envelope.Data) > 0 {
		raw = envelope.Data
	}
	var fields eventLogDetails
	_ = json.Unmarshal(raw, &fields)
	if fields.UserID == "" {
		fields.UserID = fields.OpenID
	}
	return fields
}
