package eventconv

import (
	"context"

	botgodto "github.com/WindowsSov8forUs/botgo-plus/dto"
	"github.com/satori-protocol-go/satori-go/pkg/satori/model/channel"
	"github.com/satori-protocol-go/satori-go/pkg/satori/model/event"
	"github.com/satori-protocol-go/satori-go/pkg/satori/model/guild"
	"github.com/satori-protocol-go/satori-go/pkg/satori/model/guildmember"
	satoriinteraction "github.com/satori-protocol-go/satori-go/pkg/satori/model/interaction"
	"github.com/satori-protocol-go/satori-go/pkg/satori/model/login"
	"github.com/satori-protocol-go/satori-go/pkg/satori/model/message"
	"github.com/satori-protocol-go/satori-go/pkg/satori/model/user"
)

func (c *Converter) makeInteractionEvent(
	ctx context.Context,
	loginValue *login.Login,
	data map[string]any,
) *event.Event {
	interactionValue := &botgodto.Interaction{}
	if !decodeInto(data, interactionValue) {
		return &event.Event{
			Type:      event.EventTypeInternal,
			Timestamp: pickEventTimestamp(data),
			Login:     loginValue,
		}
	}
	if interactionValue.Data == nil {
		return &event.Event{
			Type:      event.EventTypeInternal,
			Timestamp: pickEventTimestamp(data),
			Login:     loginValue,
		}
	}

	timestamp := pickEventTimestamp(data)
	if parsed, ok := parseTimestamp(interactionValue.Timestamp); ok {
		timestamp = parsed
	}

	currentLogin := loginValue
	channelValue := &channel.Channel{}
	var guildValue *guild.Guild
	var userValue *user.User
	var memberValue *guildmember.GuildMember
	switch interactionValue.ChatType {
	case 0:
		currentLogin = c.loginForPlatform(ctx, "qqguild")
		channelValue = &channel.Channel{Id: interactionValue.ChannelID, Type: channel.ChannelTypeText}
		guildValue = &guild.Guild{Id: interactionValue.GuildID}
		userID := firstNonEmpty(
			interactionValue.Data.Resolved.UserID,
			valueAsString(data["user_id"]),
		)
		if userID != "" {
			userValue = &user.User{Id: userID}
			memberValue = &guildmember.GuildMember{User: userValue}
		}
	case 1:
		currentLogin = c.loginForPlatform(ctx, "qq")
		channelValue = &channel.Channel{
			Id:   interactionValue.GroupOpenID,
			Type: channel.ChannelTypeText,
		}
		guildValue = &guild.Guild{Id: interactionValue.GroupOpenID}
		if interactionValue.GroupMemberOpenID != "" {
			userValue = &user.User{Id: interactionValue.GroupMemberOpenID}
			memberValue = &guildmember.GuildMember{User: userValue}
		}
	default:
		currentLogin = c.loginForPlatform(ctx, "qq")
		userID := firstNonEmpty(interactionValue.UserOpenID, valueAsString(data["user_openid"]))
		if userID != "" {
			channelValue = &channel.Channel{Id: "private:" + userID, Type: channel.ChannelTypeDirect}
			userValue = &user.User{Id: userID}
		}
	}

	messageValue := &message.Message{
		Id:      interactionValue.ID,
		Content: interactionValue.Data.Resolved.ButtonData,
		Channel: channelValue,
		Guild:   guildValue,
		User:    userValue,
		Member:  memberValue,
	}

	return &event.Event{
		Type:      event.EventTypeInteractionButton,
		Timestamp: timestamp,
		Login:     currentLogin,
		Button: &satoriinteraction.Button{
			Id: interactionValue.Data.Resolved.ButtonID,
		},
		Channel: channelValue,
		Guild:   guildValue,
		User:    userValue,
		Member:  memberValue,
		Message: messageValue,
		Referrer: map[string]any{
			"direct":  interactionValue.ChatType == 2,
			"msg_id":  interactionValue.ID,
			"msg_seq": -1,
		},
	}
}
