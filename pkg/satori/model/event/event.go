package event

import (
	"encoding/json"
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
	if err := json.Unmarshal(data, &wire); err != nil {
		return err
	}

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
