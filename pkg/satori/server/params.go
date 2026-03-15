package server

import (
	"mime/multipart"
	"strconv"
	"strings"
)

type OptionalInt struct {
	value int
	set   bool
}

func (v OptionalInt) Int() (int, bool) {
	return v.value, v.set
}

func (v OptionalInt) ValueOr(defaultValue int) int {
	if !v.set {
		return defaultValue
	}
	return v.value
}

func (v *OptionalInt) UnmarshalJSON(data []byte) error {
	text := strings.TrimSpace(string(data))
	if text == "" || text == "null" {
		*v = OptionalInt{}
		return nil
	}
	if strings.HasPrefix(text, "\"") && strings.HasSuffix(text, "\"") {
		text = strings.Trim(text, "\"")
		text = strings.TrimSpace(text)
		if text == "" {
			*v = OptionalInt{}
			return nil
		}
		parsed, err := strconv.Atoi(text)
		if err != nil {
			return err
		}
		*v = OptionalInt{value: parsed, set: true}
		return nil
	}

	if parsed, err := strconv.Atoi(text); err == nil {
		*v = OptionalInt{value: parsed, set: true}
		return nil
	}
	parsedFloat, err := strconv.ParseFloat(text, 64)
	if err != nil {
		return err
	}
	*v = OptionalInt{value: int(parsedFloat), set: true}
	return nil
}

type MessageReferrerParam struct {
	Direct bool        `json:"direct,omitempty"`
	MsgID  string      `json:"msg_id,omitempty"`
	MsgSeq OptionalInt `json:"msg_seq,omitempty"`
}

type MessageCreateParam struct {
	ChannelID string                `json:"channel_id"`
	Content   string                `json:"content"`
	Referrer  *MessageReferrerParam `json:"referrer,omitempty"`
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
	ChannelID string      `json:"channel_id"`
	Next      string      `json:"next,omitempty"`
	Direction string      `json:"direction,omitempty"`
	Order     string      `json:"order,omitempty"`
	Prev      string      `json:"prev,omitempty"`
	Limit     OptionalInt `json:"limit,omitempty"`
}

type ChannelParam struct {
	ChannelID string `json:"channel_id"`
}

type ChannelListParam struct {
	GuildID string `json:"guild_id"`
	Next    string `json:"next,omitempty"`
}

type ChannelCreateParam struct {
	GuildID string         `json:"guild_id"`
	Data    map[string]any `json:"data"`
}

type ChannelUpdateParam struct {
	ChannelID string         `json:"channel_id"`
	Data      map[string]any `json:"data"`
}

type ChannelMuteParam struct {
	ChannelID string      `json:"channel_id"`
	Duration  OptionalInt `json:"duration,omitempty"`
}

type UserChannelCreateParam struct {
	UserID  string `json:"user_id"`
	GuildID string `json:"guild_id,omitempty"`
}

type GuildGetParam struct {
	GuildID string `json:"guild_id"`
}

type GuildListParam struct {
	Next  string      `json:"next,omitempty"`
	Limit OptionalInt `json:"limit,omitempty"`
}

type GuildMemberGetParam struct {
	GuildID string `json:"guild_id"`
	UserID  string `json:"user_id"`
}

type GuildListByGuildParam struct {
	GuildID string      `json:"guild_id"`
	Next    string      `json:"next,omitempty"`
	Limit   OptionalInt `json:"limit,omitempty"`
}

type GuildMemberKickParam struct {
	GuildID   string `json:"guild_id"`
	UserID    string `json:"user_id"`
	Permanent bool   `json:"permanent,omitempty"`
}

type GuildMemberMuteParam struct {
	GuildID  string      `json:"guild_id"`
	UserID   string      `json:"user_id"`
	Duration OptionalInt `json:"duration,omitempty"`
}

type GuildMemberRoleParam struct {
	GuildID string `json:"guild_id"`
	UserID  string `json:"user_id"`
	RoleID  string `json:"role_id"`
}

type GuildRoleCreateParam struct {
	GuildID string         `json:"guild_id"`
	Role    map[string]any `json:"role"`
}

type GuildRoleUpdateParam struct {
	GuildID string         `json:"guild_id"`
	RoleID  string         `json:"role_id"`
	Role    map[string]any `json:"role"`
}

type GuildRoleDeleteParam struct {
	GuildID string `json:"guild_id"`
	RoleID  string `json:"role_id"`
}

type ReactionCreateParam struct {
	ChannelID string `json:"channel_id"`
	MessageID string `json:"message_id"`
	Emoji     string `json:"emoji"`
}

type ReactionDeleteParam struct {
	ChannelID string `json:"channel_id"`
	MessageID string `json:"message_id"`
	Emoji     string `json:"emoji"`
	UserID    string `json:"user_id,omitempty"`
}

type ReactionClearParam struct {
	ChannelID string `json:"channel_id"`
	MessageID string `json:"message_id"`
	Emoji     string `json:"emoji,omitempty"`
}

type ReactionListParam struct {
	ChannelID string      `json:"channel_id"`
	MessageID string      `json:"message_id"`
	Emoji     string      `json:"emoji"`
	Next      string      `json:"next,omitempty"`
	Limit     OptionalInt `json:"limit,omitempty"`
}

type UserGetParam struct {
	UserID  string `json:"user_id"`
	GuildID string `json:"guild_id,omitempty"`
}

type FriendListParam struct {
	Next string `json:"next,omitempty"`
}

type ApproveParam struct {
	MessageID string `json:"message_id"`
	Approve   bool   `json:"approve"`
	Comment   string `json:"comment,omitempty"`
}

type LoginGetParam map[string]any

type UploadCreateParam = *multipart.Form
