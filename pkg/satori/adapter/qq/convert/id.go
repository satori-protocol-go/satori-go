package convert

import (
	"fmt"
	"strings"
)

func SplitGuildCompositeID(id string) string {
	if strings.Contains(id, "_") {
		return strings.SplitN(id, "_", 2)[0]
	}
	return id
}

func SplitChannelCompositeID(id string) string {
	if strings.Contains(id, "_") {
		parts := strings.Split(id, "_")
		return parts[len(parts)-1]
	}
	return id
}

func SplitGuildUserCompositeID(id string) (string, string) {
	if strings.Contains(id, "_") {
		parts := strings.SplitN(id, "_", 2)
		return parts[0], parts[1]
	}
	return "", id
}

func ComposeChannelCompositeID(dmGuildID string, sourceGuildID string) string {
	dmGuildID = strings.TrimSpace(dmGuildID)
	sourceGuildID = strings.TrimSpace(sourceGuildID)
	if dmGuildID == "" {
		return sourceGuildID
	}
	if sourceGuildID == "" {
		return dmGuildID
	}
	return fmt.Sprintf("%s_%s", dmGuildID, sourceGuildID)
}

func ComposePrivateChannelID(userID string) string {
	userID = strings.TrimSpace(userID)
	if userID == "" {
		return "private:"
	}
	return "private:" + userID
}

func SplitPrivateChannelID(channelID string) (string, bool) {
	if strings.HasPrefix(channelID, "private:") {
		return strings.TrimPrefix(channelID, "private:"), true
	}
	return channelID, false
}
