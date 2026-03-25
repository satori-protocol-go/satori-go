package event

import (
	"fmt"

	"github.com/WindowsSov8forUs/botgo-plus/dto"
	"github.com/satori-protocol-go/satori-go/pkg/satori/model/channel"
	satorievent "github.com/satori-protocol-go/satori-go/pkg/satori/model/event"
	"github.com/satori-protocol-go/satori-go/pkg/satori/model/guild"
	"github.com/satori-protocol-go/satori-go/pkg/satori/model/guildmember"
	"github.com/satori-protocol-go/satori-go/pkg/satori/model/login"
	"github.com/satori-protocol-go/satori-go/pkg/satori/model/message"
	"github.com/satori-protocol-go/satori-go/pkg/satori/model/user"
)

func (c *Converter) makeReactionEvent(
	loginValue *login.Login,
	data map[string]any,
	eventType satorievent.EventType,
) *satorievent.Event {
	reactionValue := &dto.MessageReaction{}
	if !decodeInto(data, reactionValue) {
		return nil
	}
	if reactionValue.Target.Type != dto.ReactionTargetTypeMsg {
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

	return &satorievent.Event{
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
