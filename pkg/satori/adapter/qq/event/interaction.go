package event

import (
	"context"
	"encoding/json"

	"github.com/WindowsSov8forUs/botgo-plus/dto"
	"github.com/satori-protocol-go/satori-go/pkg/satori/adapter/qq/convert"
	"github.com/satori-protocol-go/satori-go/pkg/satori/internal/xhtml"
	"github.com/satori-protocol-go/satori-go/pkg/satori/model/channel"
	satorievent "github.com/satori-protocol-go/satori-go/pkg/satori/model/event"
	"github.com/satori-protocol-go/satori-go/pkg/satori/model/guild"
	"github.com/satori-protocol-go/satori-go/pkg/satori/model/guildmember"
	"github.com/satori-protocol-go/satori-go/pkg/satori/model/interaction"
	"github.com/satori-protocol-go/satori-go/pkg/satori/model/login"
	"github.com/satori-protocol-go/satori-go/pkg/satori/model/message"
	"github.com/satori-protocol-go/satori-go/pkg/satori/model/user"
)

func (c *Converter) makeInteractionEvent(ctx context.Context, loginValue *login.Login, data map[string]any) *satorievent.Event {
	result := &satorievent.Event{Type: satorievent.EventTypeInternal, Timestamp: pickEventTimestamp(data), Login: loginValue}
	var value dto.Interaction
	if !decodeInto(data, &value) || value.Data == nil {
		return result
	}
	var resolved struct {
		UserID       string           `json:"user_id"`
		ButtonID     string           `json:"button_id"`
		ButtonData   string           `json:"button_data"`
		FeatureID    string           `json:"feature_id"`
		MessageScene dto.MessageScene `json:"message_scene"`
	}
	if json.Unmarshal(value.Data.Resolved, &resolved) != nil {
		return result
	}
	direct := false
	switch {
	case value.Scene == "group" || value.ChatType == 1:
		result.Login = c.loginForPlatform(ctx, "qq")
		result.Channel = &channel.Channel{Id: value.GroupOpenID, Type: channel.ChannelTypeText}
		result.Guild = &guild.Guild{Id: value.GroupOpenID}
		if value.GroupMemberOpenID != "" {
			result.User = &user.User{Id: value.GroupMemberOpenID}
			result.Member = &guildmember.GuildMember{User: result.User}
		}
	case value.Scene == "c2c" || value.ChatType == 2:
		direct = true
		result.Login = c.loginForPlatform(ctx, "qq")
		result.User = &user.User{Id: value.UserOpenID}
		result.Channel = &channel.Channel{Id: "private:" + value.UserOpenID, Type: channel.ChannelTypeDirect}
	case value.Scene == "guild" || value.GuildID != "":
		result.Login = c.loginForPlatform(ctx, "qqguild")
		result.Channel = &channel.Channel{Id: value.ChannelID, Type: channel.ChannelTypeText}
		result.Guild = &guild.Guild{Id: value.GuildID}
		if resolved.UserID != "" {
			result.User = &user.User{Id: resolved.UserID}
			result.Member = &guildmember.GuildMember{User: result.User}
		}
	}
	result.Referrer = map[string]any{"direct": direct, "event_id": value.ID, "interaction_id": value.ID, "msg_seq": -1}
	if len(resolved.MessageScene.Ext) > 0 {
		result.Referrer["msg_scene"] = resolved.MessageScene
	}
	switch convert.InteractionKind(&value) {
	case 11:
		if resolved.ButtonID != "" {
			result.Type = satorievent.EventTypeInteractionButton
			result.Button = &interaction.Button{Id: resolved.ButtonID, Data: resolved.ButtonData}
		}
	case 12:
		if resolved.FeatureID != "" {
			result.Type = satorievent.EventTypeInteractionCommand
			result.Argv = &interaction.Argv{Name: resolved.FeatureID, Arguments: []any{}, Options: map[string]any{}}
		} else if resolved.ButtonData != "" {
			result.Type = satorievent.EventTypeInteractionCommand
			result.Message = &message.Message{Id: value.ID, Content: xhtml.Escape(resolved.ButtonData, false)}
		}
	}
	return result
}
