package server

import (
	"bytes"
	"encoding/json"
	"fmt"
	"mime/multipart"
	"reflect"
	"strings"

	"github.com/satori-protocol-go/satori-go/pkg/satori/model"
	"github.com/satori-protocol-go/satori-go/pkg/satori/types"
)

func validateSafeIntegerField(field string, value int64) error {
	if value < -types.MaxJSSafeInteger || value > types.MaxJSSafeInteger {
		return fmt.Errorf("%s exceeds JavaScript safe integer range", field)
	}
	return nil
}

func validateDecodedParamTags(value any) error {
	return walkValidateTags(reflect.ValueOf(value), "")
}

func walkValidateTags(value reflect.Value, fieldPath string) error {
	if !value.IsValid() {
		return nil
	}
	for value.Kind() == reflect.Interface || value.Kind() == reflect.Pointer {
		if value.IsNil() {
			return nil
		}
		value = value.Elem()
	}

	switch value.Kind() {
	case reflect.Struct:
		typ := value.Type()
		for i := 0; i < value.NumField(); i++ {
			structField := typ.Field(i)
			if structField.PkgPath != "" {
				continue
			}
			structValue := value.Field(i)
			currentPath := structField.Name
			if fieldPath != "" {
				currentPath = fieldPath + "." + structField.Name
			}
			if tag := strings.TrimSpace(structField.Tag.Get("validate")); tag != "" {
				if err := applyValidateRules(tag, structValue, currentPath); err != nil {
					return err
				}
			}
			if err := walkValidateTags(structValue, currentPath); err != nil {
				return err
			}
		}
	case reflect.Array, reflect.Slice:
		for i := 0; i < value.Len(); i++ {
			if err := walkValidateTags(value.Index(i), fieldPath); err != nil {
				return err
			}
		}
	case reflect.Map:
		iterator := value.MapRange()
		for iterator.Next() {
			if err := walkValidateTags(iterator.Value(), fieldPath); err != nil {
				return err
			}
		}
	}

	return nil
}

func applyValidateRules(tag string, value reflect.Value, fieldPath string) error {
	rules := strings.Split(tag, ",")
	for _, rawRule := range rules {
		rule := strings.TrimSpace(rawRule)
		switch rule {
		case "", "-":
			continue
		case "safe_int":
			integer, ok := valueToInt64(value)
			if !ok {
				return fmt.Errorf("%s must be an integer", fieldPath)
			}
			if err := validateSafeIntegerField(fieldPath, integer); err != nil {
				return err
			}
		default:
			return fmt.Errorf("unknown validate rule %q on %s", rule, fieldPath)
		}
	}
	return nil
}

func valueToInt64(value reflect.Value) (int64, bool) {
	if !value.IsValid() {
		return 0, false
	}
	for value.Kind() == reflect.Interface || value.Kind() == reflect.Pointer {
		if value.IsNil() {
			return 0, false
		}
		value = value.Elem()
	}

	switch value.Kind() {
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return value.Int(), true
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr:
		unsigned := value.Uint()
		if unsigned > uint64(1<<63-1) {
			return 0, false
		}
		return int64(unsigned), true
	default:
		return 0, false
	}
}

func decodeParams[T any](raw any) (T, error) {
	var params T
	if raw == nil {
		return params, nil
	}
	if casted, ok := raw.(T); ok {
		if err := validateDecodedParamTags(casted); err != nil {
			return params, BadRequest(err.Error())
		}
		return casted, nil
	}

	data, err := json.Marshal(raw)
	if err != nil {
		return params, BadRequest("invalid request params")
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	if err := decoder.Decode(&params); err != nil {
		return params, BadRequest("invalid request params")
	}
	if err := validateDecodedParamTags(params); err != nil {
		return params, BadRequest(err.Error())
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
	Duration  int64  `json:"duration" validate:"safe_int"`
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
	Duration int64  `json:"duration" validate:"safe_int"`
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

type UploadCreateParam = *multipart.Form

type InternalParam = map[string]any
