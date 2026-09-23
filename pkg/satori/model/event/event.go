package event

import (
	"encoding/json"
	"github.com/satori-protocol-go/satori-go/pkg/satori/model/emoji"
	"github.com/satori-protocol-go/satori-go/pkg/satori/model/friend"
	"github.com/satori-protocol-go/satori-go/pkg/satori/types"
	"strings"

	"github.com/satori-protocol-go/satori-go/pkg/satori/model/channel"
	"github.com/satori-protocol-go/satori-go/pkg/satori/model/guild"
	"github.com/satori-protocol-go/satori-go/pkg/satori/model/guildmember"
	"github.com/satori-protocol-go/satori-go/pkg/satori/model/guildrole"
	"github.com/satori-protocol-go/satori-go/pkg/satori/model/interaction"
	"github.com/satori-protocol-go/satori-go/pkg/satori/model/login"
	"github.com/satori-protocol-go/satori-go/pkg/satori/model/message"
	"github.com/satori-protocol-go/satori-go/pkg/satori/model/user"
)

type EventType string

const (
	EventTypeGuildEmojiAdded   EventType = "guild-emoji-added"
	EventTypeGuildEmojiUpdated EventType = "guild-emoji-updated"
	EventTypeGuildEmojiDeleted EventType = "guild-emoji-deleted"

	EventTypeFriendRequest EventType = "friend-request"

	EventTypeGuildAdded   EventType = "guild-added"
	EventTypeGuildUpdated EventType = "guild-updated"
	EventTypeGuildRemoved EventType = "guild-removed"
	EventTypeGuildRequest EventType = "guild-request"

	EventTypeGuildMemberAdded   EventType = "guild-member-added"
	EventTypeGuildMemberUpdated EventType = "guild-member-updated"
	EventTypeGuildMemberRemoved EventType = "guild-member-removed"
	EventTypeGuildMemberRequest EventType = "guild-member-request"

	EventTypeGuildRoleCreated EventType = "guild-role-created"
	EventTypeGuildRoleUpdated EventType = "guild-role-updated"
	EventTypeGuildRoleDeleted EventType = "guild-role-deleted"

	EventTypeInteractionButton  EventType = "interaction/button"
	EventTypeInteractionCommand EventType = "interaction/command"

	EventTypeLoginAdded   EventType = "login-added"
	EventTypeLoginRemoved EventType = "login-removed"
	EventTypeLoginUpdated EventType = "login-updated"

	EventTypeMessageCreated EventType = "message-created"
	EventTypeMessageUpdated EventType = "message-updated"
	EventTypeMessageDeleted EventType = "message-deleted"

	EventTypeReactionAdded   EventType = "reaction-added"
	EventTypeReactionRemoved EventType = "reaction-removed"

	EventTypeInternal EventType = "internal"
)

// Event is the canonical Satori event payload.
type Event struct {
	fields    types.FieldPresence
	Emoji     *emoji.Emoji             `json:"emoji,omitempty"`
	Friend    *friend.Friend           `json:"friend,omitempty"`
	Sn        int64                    `json:"sn"`
	Type      EventType                `json:"type"`
	Timestamp int64                    `json:"timestamp"`
	Login     *login.Login             `json:"login"`
	Argv      *interaction.Argv        `json:"argv,omitempty"`
	Button    *interaction.Button      `json:"button,omitempty"`
	Channel   *channel.Channel         `json:"channel,omitempty"`
	Guild     *guild.Guild             `json:"guild,omitempty"`
	Member    *guildmember.GuildMember `json:"member,omitempty"`
	Message   *message.Message         `json:"message,omitempty"`
	Operator  *user.User               `json:"operator,omitempty"`
	Role      *guildrole.GuildRole     `json:"role,omitempty"`
	User      *user.User               `json:"user,omitempty"`
	Referrer  map[string]any           `json:"referrer,omitempty"`
	Type_     string                   `json:"_type,omitempty"`
	Data_     any                      `json:"_data,omitempty"`
}

func (e *Event) UnmarshalJSON(data []byte) error {
	type eventWire struct {
		Emoji     *emoji.Emoji             `json:"emoji"`
		Friend    *friend.Friend           `json:"friend"`
		Sn        *int64                   `json:"sn"`
		ID        *int64                   `json:"id"`
		Type      EventType                `json:"type"`
		Timestamp int64                    `json:"timestamp"`
		Login     *login.Login             `json:"login"`
		Platform  string                   `json:"platform"`
		SelfID    string                   `json:"self_id"`
		Argv      *interaction.Argv        `json:"argv"`
		Button    *interaction.Button      `json:"button"`
		Channel   *channel.Channel         `json:"channel"`
		Guild     *guild.Guild             `json:"guild"`
		Member    *guildmember.GuildMember `json:"member"`
		Message   *message.Message         `json:"message"`
		Operator  *user.User               `json:"operator"`
		Role      *guildrole.GuildRole     `json:"role"`
		User      *user.User               `json:"user"`
		Referrer  map[string]any           `json:"referrer"`
		Type_     string                   `json:"_type"`
		Data_     any                      `json:"_data"`
	}

	var wire eventWire
	fields, err := types.DecodeFields(data, &wire)
	if err != nil {
		return err
	}
	e.fields = fields
	e.Emoji = wire.Emoji
	e.Friend = wire.Friend

	e.Sn = 0
	if wire.Sn != nil {
		e.Sn = *wire.Sn
	} else if wire.ID != nil {
		e.Sn = *wire.ID
	}

	e.Type = wire.Type
	e.Timestamp = wire.Timestamp
	e.Login = wire.Login
	e.Argv = wire.Argv
	e.Button = wire.Button
	e.Channel = wire.Channel
	e.Guild = wire.Guild
	e.Member = wire.Member
	e.Message = wire.Message
	e.Operator = wire.Operator
	e.Role = wire.Role
	e.User = wire.User
	e.Referrer = wire.Referrer
	e.Type_ = wire.Type_
	e.Data_ = wire.Data_

	selfID := strings.TrimSpace(wire.SelfID)
	platform := strings.TrimSpace(wire.Platform)
	if selfID == "" {
		return nil
	}

	if e.Login == nil {
		if platform == "" {
			platform = "unknown"
		}
		e.Login = &login.Login{
			Sn:       0,
			Platform: platform,
			User:     &user.User{Id: selfID},
			Status:   login.LoginStatusOnline,
			Adapter:  "satori",
		}
		return nil
	}

	if e.Login.User == nil {
		e.Login.User = &user.User{Id: selfID}
	}
	if e.Login.Platform == "" && platform != "" {
		e.Login.Platform = platform
	}
	return nil
}

// MarshalJSON applies Satori resource promotion only to a temporary wire view.
func (e Event) MarshalJSON() ([]byte, error) {
	if e.Message != nil {
		if e.Channel == nil && !e.fields.Has("channel") {
			e.Channel = e.Message.Channel
		}
		if e.Guild == nil && !e.fields.Has("guild") {
			e.Guild = e.Message.Guild
		}
		if e.Member == nil && !e.fields.Has("member") {
			e.Member = e.Message.Member
		}
	}
	if e.User == nil && !e.fields.Has("user") {
		if e.Member != nil {
			e.User = e.Member.User
		}
		if e.User == nil && e.Message != nil {
			e.User = e.Message.User
		}
		if e.User == nil && e.Friend != nil {
			e.User = e.Friend.User
		}
	}
	if e.Message != nil {
		e.Message = e.Message.WithoutResources()
	}
	if e.Member != nil {
		e.Member = e.Member.WithoutUser()
	}
	if e.Friend != nil {
		e.Friend = e.Friend.WithoutUser()
	}
	out := map[string]any{"sn": e.Sn, "type": e.Type, "timestamp": e.Timestamp, "login": e.Login}
	if e.Login != nil && e.Type != EventTypeLoginAdded && e.Type != EventTypeLoginUpdated && e.Type != EventTypeLoginRemoved {
		out["login"] = map[string]any{"sn": e.Login.Sn, "platform": e.Login.Platform, "user": e.Login.User}
	}
	e.fields.Put(out, "argv", e.Argv, e.Argv != nil)
	e.fields.Put(out, "button", e.Button, e.Button != nil)
	e.fields.Put(out, "channel", e.Channel, e.Channel != nil)
	e.fields.Put(out, "emoji", e.Emoji, e.Emoji != nil)
	e.fields.Put(out, "friend", e.Friend, e.Friend != nil)
	e.fields.Put(out, "guild", e.Guild, e.Guild != nil)
	e.fields.Put(out, "member", e.Member, e.Member != nil)
	e.fields.Put(out, "message", e.Message, e.Message != nil)
	e.fields.Put(out, "operator", e.Operator, e.Operator != nil)
	e.fields.Put(out, "role", e.Role, e.Role != nil)
	e.fields.Put(out, "user", e.User, e.User != nil)
	e.fields.Put(out, "referrer", e.Referrer, e.Referrer != nil)
	e.fields.Put(out, "_type", e.Type_, e.Type_ != "")
	e.fields.Put(out, "_data", e.Data_, e.Data_ != nil)
	return json.Marshal(out)
}
