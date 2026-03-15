package codec

import (
	"strconv"
	"strings"

	botgodto "github.com/WindowsSov8forUs/botgo-plus/dto"
	"github.com/satori-protocol-go/satori-go/pkg/satori/model/channel"
)

func ParseReactionEmoji(raw string) botgodto.Emoji {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return botgodto.Emoji{Type: 1}
	}
	if strings.Contains(raw, ":") {
		parts := strings.SplitN(raw, ":", 2)
		typ := 1
		if number, err := strconv.Atoi(parts[0]); err == nil {
			typ = number
		}
		return botgodto.Emoji{Type: typ, ID: parts[1]}
	}
	return botgodto.Emoji{Type: 1, ID: raw}
}

func ParseChannelValue(raw map[string]any) *botgodto.ChannelValueObject {
	value := &botgodto.ChannelValueObject{}
	value.Name = optionalString(raw, "name")
	value.ParentID = optionalString(raw, "parent_id")
	if typ, ok := numberToInt(raw["type"]); ok {
		switch channel.ChannelType(typ) {
		case channel.ChannelTypeVoice:
			value.Type = botgodto.ChannelTypeVoice
		case channel.ChannelTypeCategory:
			value.Type = botgodto.ChannelTypeCategory
		default:
			value.Type = botgodto.ChannelTypeText
		}
	}
	return value
}

func ParseRole(raw map[string]any) *botgodto.Role {
	role := &botgodto.Role{}
	role.Name = optionalString(raw, "name")
	if color, ok := numberToUint32(raw["color"]); ok {
		role.Color = color
	}
	if hoist, ok := numberToUint32(raw["hoist"]); ok {
		role.Hoist = hoist
	}
	return role
}
