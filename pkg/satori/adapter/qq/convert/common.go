package convert

import (
	"github.com/WindowsSov8forUs/botgo-plus/dto"
	"strings"
)

func firstNonEmpty(items ...string) string {
	for _, item := range items {
		if strings.TrimSpace(item) != "" {
			return item
		}
	}
	return ""
}

func InteractionKind(value *dto.Interaction) uint32 {
	if value == nil {
		return 0
	}
	if value.Data != nil && value.Data.Type != 0 {
		return uint32(value.Data.Type)
	}
	return uint32(value.Type)
}
