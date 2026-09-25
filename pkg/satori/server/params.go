package server

import (
	"encoding/json"

	"github.com/satori-protocol-go/satori-go/pkg/satori/model"
	"github.com/satori-protocol-go/satori-go/pkg/satori/protocol"
	"github.com/satori-protocol-go/satori-go/pkg/satori/types"
)

func decodeParams[T any](raw any) (T, error) {
	var params T
	if raw == nil {
		return params, nil
	}
	if casted, ok := raw.(T); ok {
		return casted, nil
	}

	data, err := json.Marshal(raw)
	if err != nil {
		return params, BadRequest("invalid request params")
	}
	decoded, err := protocol.DecodeJSONBytes(data, &params)
	if err != nil {
		return params, BadRequest("invalid request params")
	}
	if !decoded {
		return params, BadRequest("invalid request params")
	}
	return params, nil
}

type ChannelParam struct {
	ChannelID string `json:"channel_id"`
}

type ChannelListParam struct {
	GuildID string               `json:"guild_id"`
	Next    types.Option[string] `json:"next"`
}

type ChannelCreateParam struct {
	GuildID string        `json:"guild_id"`
	Data    model.Channel `json:"data"`
}

type ChannelUpdateParam struct {
	ChannelID string        `json:"channel_id"`
	Data      model.Channel `json:"data"`
}

type ChannelMuteParam struct {
	ChannelID string `json:"channel_id"`
	Duration  int64  `json:"duration"`
}

type UserChannelCreateParam struct {
	UserID  string               `json:"user_id"`
	GuildID types.Option[string] `json:"guild_id"`
}

type FriendListParam struct {
	Next types.Option[string] `json:"next"`
}

type FriendDeleteParam struct {
	UserID string `json:"user_id"`
}

type ApproveParam struct {
	MessageID string               `json:"message_id"`
	Approve   bool                 `json:"approve"`
	Comment   types.Option[string] `json:"comment"`
}

type GuildGetParam struct {
	GuildID string `json:"guild_id"`
}

type GuildListParam struct {
	Next types.Option[string] `json:"next"`
}

type GuildMemberGetParam struct {
	GuildID string `json:"guild_id"`
	UserID  string `json:"user_id"`
}

type GuildListByGuildParam struct {
	GuildID string               `json:"guild_id"`
	Next    types.Option[string] `json:"next"`
}

type GuildMemberKickParam struct {
	GuildID   string             `json:"guild_id"`
	UserID    string             `json:"user_id"`
	Permanent types.Option[bool] `json:"permanent"`
}

type GuildMemberMuteParam struct {
	GuildID  string `json:"guild_id"`
	UserID   string `json:"user_id"`
	Duration int64  `json:"duration"`
}

type GuildMemberRoleParam struct {
	GuildID string `json:"guild_id"`
	UserID  string `json:"user_id"`
	RoleID  string `json:"role_id"`
}

type GuildRoleCreateParam struct {
	GuildID string          `json:"guild_id"`
	Role    model.GuildRole `json:"role"`
}

type GuildRoleUpdateParam struct {
	GuildID string          `json:"guild_id"`
	RoleID  string          `json:"role_id"`
	Role    model.GuildRole `json:"role"`
}

type GuildRoleDeleteParam struct {
	GuildID string `json:"guild_id"`
	RoleID  string `json:"role_id"`
}

type LoginGetParam struct{}

type MessageCreateParam struct {
	ChannelID string                       `json:"channel_id"`
	Content   string                       `json:"content"`
	Referrer  types.Option[map[string]any] `json:"referrer"`
}

type MessageOpParam struct {
	ChannelID string `json:"channel_id"`
	MessageID string `json:"message_id"`
}

type MessageUpdateParam struct {
	ChannelID string `json:"channel_id"`
	MessageID string `json:"message_id"`
	Content   string `json:"content"`
}

type MessageListParam struct {
	ChannelID string                        `json:"channel_id"`
	Next      types.Option[string]          `json:"next"`
	Direction types.Option[model.Direction] `json:"direction"`
	Limit     types.Option[int64]           `json:"limit"`
	Order     types.Option[model.Order]     `json:"order"`
}

type ReactionCreateParam struct {
	ChannelID string `json:"channel_id"`
	MessageID string `json:"message_id"`
	EmojiID   string `json:"emoji_id"`
}

type ReactionDeleteParam struct {
	ChannelID string               `json:"channel_id"`
	MessageID string               `json:"message_id"`
	EmojiID   string               `json:"emoji_id"`
	UserID    types.Option[string] `json:"user_id"`
}

type ReactionClearParam struct {
	ChannelID string               `json:"channel_id"`
	MessageID string               `json:"message_id"`
	EmojiID   types.Option[string] `json:"emoji_id"`
}

type ReactionListParam struct {
	ChannelID string               `json:"channel_id"`
	MessageID string               `json:"message_id"`
	EmojiID   string               `json:"emoji_id"`
	Next      types.Option[string] `json:"next"`
}

type UserGetParam struct {
	UserID string `json:"user_id"`
}

type UploadCreateParam map[string]UploadFile

type InternalParam = map[string]any
