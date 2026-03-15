package server

import (
	"bytes"
	"encoding/json"
	"fmt"
	"mime/multipart"
	"strings"
)

const maxJSSafeInteger int64 = 9007199254740991

func validateSafeIntegerField(field string, value int64) error {
	if value < -maxJSSafeInteger || value > maxJSSafeInteger {
		return fmt.Errorf("%s exceeds JavaScript safe integer range", field)
	}
	return nil
}

func decodeParamJSON(data []byte, target any) error {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	return decoder.Decode(target)
}

type MessageReferrerParam struct {
	Direct Option[bool]   `json:"direct"`
	MsgID  Option[string] `json:"msg_id"`
	MsgSeq Option[int64]  `json:"msg_seq"`
}

func (p *MessageReferrerParam) UnmarshalJSON(data []byte) error {
	type raw struct {
		Direct *bool   `json:"direct"`
		MsgID  *string `json:"msg_id"`
		MsgSeq *int64  `json:"msg_seq"`
	}
	value := raw{}
	if err := decodeParamJSON(data, &value); err != nil {
		return err
	}
	p.Direct = optionFromPointer(value.Direct)
	if value.MsgID == nil {
		p.MsgID = None[string]()
	} else {
		p.MsgID = Some(strings.TrimSpace(*value.MsgID))
	}
	if value.MsgSeq == nil {
		p.MsgSeq = None[int64]()
	} else {
		if err := validateSafeIntegerField("msg_seq", *value.MsgSeq); err != nil {
			return err
		}
		p.MsgSeq = Some(*value.MsgSeq)
	}
	return nil
}

type MessageCreateParam struct {
	ChannelID string                       `json:"channel_id"`
	Content   string                       `json:"content"`
	Referrer  Option[MessageReferrerParam] `json:"referrer"`
}

func (p *MessageCreateParam) UnmarshalJSON(data []byte) error {
	type raw struct {
		ChannelID string                `json:"channel_id"`
		Content   string                `json:"content"`
		Referrer  *MessageReferrerParam `json:"referrer"`
	}
	value := raw{}
	if err := decodeParamJSON(data, &value); err != nil {
		return err
	}
	p.ChannelID = value.ChannelID
	p.Content = value.Content
	p.Referrer = optionFromPointer(value.Referrer)
	return nil
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
	ChannelID string         `json:"channel_id"`
	Next      Option[string] `json:"next"`
	Direction Option[string] `json:"direction"`
	Limit     Option[int64]  `json:"limit"`
	Order     Option[string] `json:"order"`
}

func (p *MessageListParam) UnmarshalJSON(data []byte) error {
	type raw struct {
		ChannelID string  `json:"channel_id"`
		Next      *string `json:"next"`
		Direction *string `json:"direction"`
		Limit     *int64  `json:"limit"`
		Order     *string `json:"order"`
	}
	value := raw{}
	if err := decodeParamJSON(data, &value); err != nil {
		return err
	}
	p.ChannelID = value.ChannelID
	if value.Next == nil {
		p.Next = None[string]()
	} else {
		p.Next = Some(strings.TrimSpace(*value.Next))
	}
	if value.Direction == nil {
		p.Direction = None[string]()
	} else {
		p.Direction = Some(strings.TrimSpace(*value.Direction))
	}
	if value.Limit == nil {
		p.Limit = None[int64]()
	} else {
		if err := validateSafeIntegerField("limit", *value.Limit); err != nil {
			return err
		}
		p.Limit = Some(*value.Limit)
	}
	if value.Order == nil {
		p.Order = None[string]()
	} else {
		p.Order = Some(strings.TrimSpace(*value.Order))
	}
	return nil
}

type ChannelParam struct {
	ChannelID string `json:"channel_id"`
}

type ChannelListParam struct {
	GuildID string         `json:"guild_id"`
	Next    Option[string] `json:"next"`
}

func (p *ChannelListParam) UnmarshalJSON(data []byte) error {
	type raw struct {
		GuildID string  `json:"guild_id"`
		Next    *string `json:"next"`
	}
	value := raw{}
	if err := decodeParamJSON(data, &value); err != nil {
		return err
	}
	p.GuildID = value.GuildID
	if value.Next == nil {
		p.Next = None[string]()
	} else {
		p.Next = Some(strings.TrimSpace(*value.Next))
	}
	return nil
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
	ChannelID string `json:"channel_id"`
	Duration  int64  `json:"duration"`
}

func (p *ChannelMuteParam) UnmarshalJSON(data []byte) error {
	type raw struct {
		ChannelID string `json:"channel_id"`
		Duration  int64  `json:"duration"`
	}
	value := raw{}
	if err := decodeParamJSON(data, &value); err != nil {
		return err
	}
	if err := validateSafeIntegerField("duration", value.Duration); err != nil {
		return err
	}
	p.ChannelID = value.ChannelID
	p.Duration = value.Duration
	return nil
}

type UserChannelCreateParam struct {
	UserID  string         `json:"user_id"`
	GuildID Option[string] `json:"guild_id"`
}

func (p *UserChannelCreateParam) UnmarshalJSON(data []byte) error {
	type raw struct {
		UserID  string  `json:"user_id"`
		GuildID *string `json:"guild_id"`
	}
	value := raw{}
	if err := decodeParamJSON(data, &value); err != nil {
		return err
	}
	p.UserID = value.UserID
	if value.GuildID == nil {
		p.GuildID = None[string]()
	} else {
		p.GuildID = Some(strings.TrimSpace(*value.GuildID))
	}
	return nil
}

type GuildGetParam struct {
	GuildID string `json:"guild_id"`
}

type GuildListParam struct {
	Next Option[string] `json:"next"`
}

func (p *GuildListParam) UnmarshalJSON(data []byte) error {
	type raw struct {
		Next *string `json:"next"`
	}
	value := raw{}
	if err := decodeParamJSON(data, &value); err != nil {
		return err
	}
	if value.Next == nil {
		p.Next = None[string]()
	} else {
		p.Next = Some(strings.TrimSpace(*value.Next))
	}
	return nil
}

type GuildMemberGetParam struct {
	GuildID string `json:"guild_id"`
	UserID  string `json:"user_id"`
}

type GuildListByGuildParam struct {
	GuildID string         `json:"guild_id"`
	Next    Option[string] `json:"next"`
}

func (p *GuildListByGuildParam) UnmarshalJSON(data []byte) error {
	type raw struct {
		GuildID string  `json:"guild_id"`
		Next    *string `json:"next"`
	}
	value := raw{}
	if err := decodeParamJSON(data, &value); err != nil {
		return err
	}
	p.GuildID = value.GuildID
	if value.Next == nil {
		p.Next = None[string]()
	} else {
		p.Next = Some(strings.TrimSpace(*value.Next))
	}
	return nil
}

type GuildMemberKickParam struct {
	GuildID   string       `json:"guild_id"`
	UserID    string       `json:"user_id"`
	Permanent Option[bool] `json:"permanent"`
}

func (p *GuildMemberKickParam) UnmarshalJSON(data []byte) error {
	type raw struct {
		GuildID   string `json:"guild_id"`
		UserID    string `json:"user_id"`
		Permanent *bool  `json:"permanent"`
	}
	value := raw{}
	if err := decodeParamJSON(data, &value); err != nil {
		return err
	}
	p.GuildID = value.GuildID
	p.UserID = value.UserID
	p.Permanent = optionFromPointer(value.Permanent)
	return nil
}

type GuildMemberMuteParam struct {
	GuildID  string `json:"guild_id"`
	UserID   string `json:"user_id"`
	Duration int64  `json:"duration"`
}

func (p *GuildMemberMuteParam) UnmarshalJSON(data []byte) error {
	type raw struct {
		GuildID  string `json:"guild_id"`
		UserID   string `json:"user_id"`
		Duration int64  `json:"duration"`
	}
	value := raw{}
	if err := decodeParamJSON(data, &value); err != nil {
		return err
	}
	if err := validateSafeIntegerField("duration", value.Duration); err != nil {
		return err
	}
	p.GuildID = value.GuildID
	p.UserID = value.UserID
	p.Duration = value.Duration
	return nil
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
	ChannelID string         `json:"channel_id"`
	MessageID string         `json:"message_id"`
	Emoji     string         `json:"emoji"`
	UserID    Option[string] `json:"user_id"`
}

func (p *ReactionDeleteParam) UnmarshalJSON(data []byte) error {
	type raw struct {
		ChannelID string  `json:"channel_id"`
		MessageID string  `json:"message_id"`
		Emoji     string  `json:"emoji"`
		UserID    *string `json:"user_id"`
	}
	value := raw{}
	if err := decodeParamJSON(data, &value); err != nil {
		return err
	}
	p.ChannelID = value.ChannelID
	p.MessageID = value.MessageID
	p.Emoji = value.Emoji
	if value.UserID == nil {
		p.UserID = None[string]()
	} else {
		p.UserID = Some(strings.TrimSpace(*value.UserID))
	}
	return nil
}

type ReactionClearParam struct {
	ChannelID string         `json:"channel_id"`
	MessageID string         `json:"message_id"`
	Emoji     Option[string] `json:"emoji"`
}

func (p *ReactionClearParam) UnmarshalJSON(data []byte) error {
	type raw struct {
		ChannelID string  `json:"channel_id"`
		MessageID string  `json:"message_id"`
		Emoji     *string `json:"emoji"`
	}
	value := raw{}
	if err := decodeParamJSON(data, &value); err != nil {
		return err
	}
	p.ChannelID = value.ChannelID
	p.MessageID = value.MessageID
	if value.Emoji == nil {
		p.Emoji = None[string]()
	} else {
		p.Emoji = Some(strings.TrimSpace(*value.Emoji))
	}
	return nil
}

type ReactionListParam struct {
	ChannelID string         `json:"channel_id"`
	MessageID string         `json:"message_id"`
	Emoji     string         `json:"emoji"`
	Next      Option[string] `json:"next"`
}

func (p *ReactionListParam) UnmarshalJSON(data []byte) error {
	type raw struct {
		ChannelID string  `json:"channel_id"`
		MessageID string  `json:"message_id"`
		Emoji     string  `json:"emoji"`
		Next      *string `json:"next"`
	}
	value := raw{}
	if err := decodeParamJSON(data, &value); err != nil {
		return err
	}
	p.ChannelID = value.ChannelID
	p.MessageID = value.MessageID
	p.Emoji = value.Emoji
	if value.Next == nil {
		p.Next = None[string]()
	} else {
		p.Next = Some(strings.TrimSpace(*value.Next))
	}
	return nil
}

type UserGetParam struct {
	UserID string `json:"user_id"`
}

type FriendListParam struct {
	Next Option[string] `json:"next"`
}

func (p *FriendListParam) UnmarshalJSON(data []byte) error {
	type raw struct {
		Next *string `json:"next"`
	}
	value := raw{}
	if err := decodeParamJSON(data, &value); err != nil {
		return err
	}
	if value.Next == nil {
		p.Next = None[string]()
	} else {
		p.Next = Some(strings.TrimSpace(*value.Next))
	}
	return nil
}

type ApproveParam struct {
	MessageID string         `json:"message_id"`
	Approve   bool           `json:"approve"`
	Comment   Option[string] `json:"comment"`
}

func (p *ApproveParam) UnmarshalJSON(data []byte) error {
	type raw struct {
		MessageID string  `json:"message_id"`
		Approve   bool    `json:"approve"`
		Comment   *string `json:"comment"`
	}
	value := raw{}
	if err := decodeParamJSON(data, &value); err != nil {
		return err
	}
	p.MessageID = value.MessageID
	p.Approve = value.Approve
	if value.Comment == nil {
		p.Comment = None[string]()
	} else {
		p.Comment = Some(strings.TrimSpace(*value.Comment))
	}
	return nil
}

type LoginGetParam map[string]any

type UploadCreateParam = *multipart.Form
