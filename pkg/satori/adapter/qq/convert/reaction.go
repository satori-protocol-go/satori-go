package convert

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/WindowsSov8forUs/botgo-plus/dto"
	"github.com/satori-protocol-go/satori-go/pkg/satori/model/emoji"
)

func ParseReactionEmoji(raw string) dto.Emoji {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return dto.Emoji{Type: 1}
	}
	if strings.Contains(raw, ":") {
		parts := strings.SplitN(raw, ":", 2)
		typ := 1
		if number, err := strconv.Atoi(parts[0]); err == nil {
			typ = number
		}
		return dto.Emoji{Type: typ, ID: parts[1]}
	}
	return dto.Emoji{Type: 1, ID: raw}
}

func ToSatoriEmoji(item dto.Emoji) *emoji.Emoji {
	if item.ID == "" {
		return nil
	}
	name := item.ID
	if item.Type != 1 {
		name = fmt.Sprintf("%d:%s", item.Type, item.ID)
	}
	return &emoji.Emoji{Id: item.ID, Name: name}
}
