package eventconv

import (
	"fmt"

	botgodto "github.com/WindowsSov8forUs/botgo-plus/dto"
	"github.com/satori-protocol-go/satori-go/pkg/satori/model/channel"
	"github.com/satori-protocol-go/satori-go/pkg/satori/model/event"
	"github.com/satori-protocol-go/satori-go/pkg/satori/model/guild"
	"github.com/satori-protocol-go/satori-go/pkg/satori/model/guildmember"
	"github.com/satori-protocol-go/satori-go/pkg/satori/model/login"
	"github.com/satori-protocol-go/satori-go/pkg/satori/model/message"
	"github.com/satori-protocol-go/satori-go/pkg/satori/model/user"
)

func (c *Converter) makeGuildMessageCreatedEvent(loginValue *login.Login, data map[string]any) *event.Event {
	msg := decodeDTOMessage(data)
	satoriMessage := c.messageFromDTO(msg, "qqguild")
	if satoriMessage.Id == "" {
		satoriMessage.Id = valueAsString(data["id"])
	}
	if satoriMessage.CreateAt == 0 {
		satoriMessage.CreateAt = pickEventTimestamp(data)
	}
	return &event.Event{
		Type:      event.EventTypeMessageCreated,
		Timestamp: pickTimestampOrDefault(satoriMessage.CreateAt, data),
		Login:     loginValue,
		Channel:   satoriMessage.Channel,
		Guild:     satoriMessage.Guild,
		Member:    satoriMessage.Member,
		User:      satoriMessage.User,
		Message:   satoriMessage,
		Referrer: map[string]any{
			"msg_id":  satoriMessage.Id,
			"msg_seq": -1,
		},
	}
}

func (c *Converter) makeGuildDirectMessageCreatedEvent(loginValue *login.Login, data map[string]any) *event.Event {
	msg := decodeDTOMessage(data)
	satoriMessage := c.messageFromDTO(msg, "qqguild")
	dmGuildID := firstNonEmpty(valueAsString(data["guild_id"]), msg.GuildID)
	channelID := firstNonEmpty(valueAsString(data["channel_id"]), msg.ChannelID)
	srcGuildID := valueAsString(data["src_guild_id"])

	if dmGuildID != "" && channelID != "" {
		satoriMessage.Channel = &channel.Channel{
			Id:   dmGuildID + "_" + channelID,
			Type: channel.ChannelTypeDirect,
		}
	}
	if srcGuildID != "" && dmGuildID != "" {
		satoriMessage.Guild = &guild.Guild{Id: srcGuildID + "_" + dmGuildID}
	}
	if satoriMessage.Id == "" {
		satoriMessage.Id = valueAsString(data["id"])
	}
	return &event.Event{
		Type:      event.EventTypeMessageCreated,
		Timestamp: pickTimestampOrDefault(satoriMessage.CreateAt, data),
		Login:     loginValue,
		Channel:   satoriMessage.Channel,
		Guild:     satoriMessage.Guild,
		Member:    satoriMessage.Member,
		User:      satoriMessage.User,
		Message:   satoriMessage,
		Referrer: map[string]any{
			"direct":  true,
			"msg_id":  satoriMessage.Id,
			"msg_seq": -1,
		},
	}
}

func (c *Converter) makeGroupMessageCreatedEvent(loginValue *login.Login, data map[string]any) *event.Event {
	msg := decodeDTOMessage(data)
	satoriMessage := c.messageFromDTO(msg, "qq")
	groupID := firstNonEmpty(
		valueAsString(data["group_openid"]),
		valueAsString(data["group_id"]),
		msg.GroupID,
	)
	if groupID != "" {
		satoriMessage.Channel = &channel.Channel{Id: groupID, Type: channel.ChannelTypeText}
		satoriMessage.Guild = &guild.Guild{Id: groupID}
	}
	if satoriMessage.User == nil {
		satoriMessage.User = c.userFromDTO(msg.Author)
	}
	if satoriMessage.Member == nil && satoriMessage.User != nil {
		satoriMessage.Member = &guildmember.GuildMember{
			User:   satoriMessage.User,
			Avatar: satoriMessage.User.Avatar,
		}
	}
	if satoriMessage.Id == "" {
		satoriMessage.Id = valueAsString(data["id"])
	}
	return &event.Event{
		Type:      event.EventTypeMessageCreated,
		Timestamp: pickTimestampOrDefault(satoriMessage.CreateAt, data),
		Login:     loginValue,
		Channel:   satoriMessage.Channel,
		Guild:     satoriMessage.Guild,
		Member:    satoriMessage.Member,
		User:      satoriMessage.User,
		Message:   satoriMessage,
		Referrer: map[string]any{
			"msg_id":  satoriMessage.Id,
			"msg_seq": -1,
		},
	}
}

func (c *Converter) makeC2CMessageCreatedEvent(loginValue *login.Login, data map[string]any) *event.Event {
	msg := decodeDTOMessage(data)
	satoriMessage := c.messageFromDTO(msg, "qq")
	if satoriMessage.User == nil {
		satoriMessage.User = c.userFromDTO(msg.Author)
	}
	if satoriMessage.User != nil && satoriMessage.User.Id != "" {
		satoriMessage.Channel = &channel.Channel{
			Id:   "private:" + satoriMessage.User.Id,
			Type: channel.ChannelTypeDirect,
		}
	}
	if satoriMessage.Id == "" {
		satoriMessage.Id = valueAsString(data["id"])
	}
	return &event.Event{
		Type:      event.EventTypeMessageCreated,
		Timestamp: pickTimestampOrDefault(satoriMessage.CreateAt, data),
		Login:     loginValue,
		Channel:   satoriMessage.Channel,
		User:      satoriMessage.User,
		Message:   satoriMessage,
		Referrer: map[string]any{
			"direct":  true,
			"msg_id":  satoriMessage.Id,
			"msg_seq": -1,
		},
	}
}

func (c *Converter) makeGuildMessageDeletedEvent(loginValue *login.Login, data map[string]any) *event.Event {
	rawMessage := objectValue(data["message"])
	dtoMessage := decodeDTOMessage(rawMessage)
	satoriMessage := &message.Message{Id: dtoMessage.ID}
	if dtoMessage.ChannelID != "" {
		satoriMessage.Channel = &channel.Channel{Id: dtoMessage.ChannelID, Type: channel.ChannelTypeText}
	}
	if dtoMessage.GuildID != "" {
		satoriMessage.Guild = &guild.Guild{Id: dtoMessage.GuildID}
	}
	satoriMessage.User = c.userFromDTO(dtoMessage.Author)
	satoriMessage.Member = c.memberFromDTO(dtoMessage.Member)
	operator := decodeDTOUser(c, objectValue(data["op_user"]))

	return &event.Event{
		Type:      event.EventTypeMessageDeleted,
		Timestamp: pickEventTimestamp(data),
		Login:     loginValue,
		Channel:   satoriMessage.Channel,
		Guild:     satoriMessage.Guild,
		Member:    satoriMessage.Member,
		User:      satoriMessage.User,
		Operator:  operator,
		Message:   satoriMessage,
		Referrer: map[string]any{
			"direct":  false,
			"msg_id":  satoriMessage.Id,
			"msg_seq": -1,
		},
	}
}

func (c *Converter) makeDirectMessageDeletedEvent(loginValue *login.Login, data map[string]any) *event.Event {
	rawMessage := objectValue(data["message"])
	dtoMessage := decodeDTOMessage(rawMessage)
	messageID := firstNonEmpty(dtoMessage.ID, valueAsString(rawMessage["id"]))
	dmGuildID := firstNonEmpty(dtoMessage.GuildID, valueAsString(rawMessage["guild_id"]))
	srcGuildID := valueAsString(rawMessage["src_guild_id"])
	channelID := firstNonEmpty(dtoMessage.ChannelID, valueAsString(rawMessage["channel_id"]))

	channelValue := &channel.Channel{Type: channel.ChannelTypeDirect}
	if dmGuildID != "" && channelID != "" {
		channelValue.Id = dmGuildID + "_" + channelID
	}
	var guildValue *guild.Guild
	if srcGuildID != "" && dmGuildID != "" {
		guildValue = &guild.Guild{Id: srcGuildID + "_" + dmGuildID}
	}

	userValue := c.userFromDTO(dtoMessage.Author)
	operator := decodeDTOUser(c, objectValue(data["op_user"]))
	satoriMessage := &message.Message{
		Id:      messageID,
		Channel: channelValue,
		Guild:   guildValue,
		User:    userValue,
	}

	return &event.Event{
		Type:      event.EventTypeMessageDeleted,
		Timestamp: pickEventTimestamp(data),
		Login:     loginValue,
		Channel:   channelValue,
		Guild:     guildValue,
		User:      userValue,
		Operator:  operator,
		Message:   satoriMessage,
		Referrer: map[string]any{
			"direct":  true,
			"msg_id":  messageID,
			"msg_seq": -1,
		},
	}
}

func (c *Converter) makeReactionEvent(
	loginValue *login.Login,
	data map[string]any,
	eventType event.EventType,
) *event.Event {
	reactionValue := &botgodto.MessageReaction{}
	if !decodeInto(data, reactionValue) {
		return nil
	}
	if reactionValue.Target.Type != botgodto.ReactionTargetTypeMsg {
		return nil
	}
	emojiID := reactionValue.Emoji.ID
	if reactionValue.Emoji.Type != 1 {
		emojiID = fmt.Sprintf("%d:%s", reactionValue.Emoji.Type, reactionValue.Emoji.ID)
	}

	channelValue := &channel.Channel{Id: reactionValue.ChannelID, Type: channel.ChannelTypeText}
	guildValue := &guild.Guild{Id: reactionValue.GuildID}
	userValue := &user.User{Id: reactionValue.UserID}
	memberValue := &guildmember.GuildMember{User: userValue}
	messageValue := &message.Message{
		Id:      reactionValue.Target.ID,
		Content: fmt.Sprintf(`<chronocat:emoji id="%s"/>`, emojiID),
		Channel: channelValue,
		Guild:   guildValue,
		User:    userValue,
		Member:  memberValue,
	}

	return &event.Event{
		Type:      eventType,
		Timestamp: pickEventTimestamp(data),
		Login:     loginValue,
		Channel:   channelValue,
		Guild:     guildValue,
		User:      userValue,
		Member:    memberValue,
		Message:   messageValue,
	}
}
