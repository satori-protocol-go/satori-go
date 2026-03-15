package codec

import (
	"fmt"
	"regexp"
	"strings"

	botgodto "github.com/WindowsSov8forUs/botgo-plus/dto"
	"github.com/satori-protocol-go/satori-go/pkg/satori/model/channel"
	"github.com/satori-protocol-go/satori-go/pkg/satori/model/guild"
	"github.com/satori-protocol-go/satori-go/pkg/satori/model/message"
)

var (
	mentionPattern = regexp.MustCompile(`<@!?(\w+)>`)
	channelPattern = regexp.MustCompile(`<#(\w+)>`)
	emojiPattern   = regexp.MustCompile(`<emoji:(\w+)>`)
)

func MessageFromDTO(input *botgodto.Message, platform string) *message.Message {
	if input == nil {
		return &message.Message{}
	}
	result := &message.Message{
		Id:      input.ID,
		Content: messageContentFromDTO(input),
	}

	if createdAt, err := input.Timestamp.Time(); err == nil {
		result.CreateAt = createdAt.UnixMilli()
	}
	if updatedAt, err := input.EditedTimestamp.Time(); err == nil {
		result.UpdateAt = updatedAt.UnixMilli()
	}

	result.User = UserFromDTO(input.Author)
	result.Member = MemberFromDTO(input.Member)

	switch {
	case input.ChannelID != "":
		channelType := channel.ChannelTypeText
		if input.DirectMessage {
			channelType = channel.ChannelTypeDirect
		}
		result.Channel = &channel.Channel{Id: input.ChannelID, Type: channelType}
		if input.GuildID != "" {
			result.Guild = &guild.Guild{Id: input.GuildID}
		}
	case input.GroupID != "":
		result.Channel = &channel.Channel{Id: input.GroupID, Type: channel.ChannelTypeText}
		result.Guild = &guild.Guild{Id: input.GroupID}
	default:
		derivedID := ""
		if result.User != nil {
			derivedID = result.User.Id
		}
		if derivedID == "" && input.Author != nil {
			derivedID = firstNonEmpty(input.Author.UserOpenID, input.Author.MemberOpenID, input.Author.ID)
		}
		if derivedID != "" {
			channelType := channel.ChannelTypeDirect
			if platform == "qqguild" {
				channelType = channel.ChannelTypeText
			}
			result.Channel = &channel.Channel{Id: derivedID, Type: channelType}
		}
	}

	return result
}

func messageContentFromDTO(input *botgodto.Message) string {
	if input == nil {
		return ""
	}
	content := strings.TrimSpace(input.Content)
	if content != "" {
		content = mentionPattern.ReplaceAllString(content, `<at id="$1"/>`)
		content = channelPattern.ReplaceAllString(content, `<sharp id="$1"/>`)
		content = emojiPattern.ReplaceAllString(content, `<chronocat:emoji id="$1"/>`)
	}

	chunks := []string{}
	if content != "" {
		chunks = append(chunks, content)
	}
	if input.MessageReference != nil && strings.TrimSpace(input.MessageReference.MessageID) != "" {
		chunks = append(chunks, fmt.Sprintf(`<quote id="%s"/>`, input.MessageReference.MessageID))
	}
	for _, item := range input.Attachments {
		if item == nil || strings.TrimSpace(item.URL) == "" {
			continue
		}
		src := item.URL
		if !strings.HasPrefix(src, "http://") && !strings.HasPrefix(src, "https://") {
			src = "https://" + strings.TrimPrefix(src, "//")
		}
		switch {
		case strings.HasPrefix(item.ContentType, "image"):
			chunks = append(chunks, fmt.Sprintf(`<img src="%s"/>`, src))
		case strings.HasPrefix(item.ContentType, "audio"):
			chunks = append(chunks, fmt.Sprintf(`<audio src="%s"/>`, src))
		case strings.HasPrefix(item.ContentType, "video"):
			chunks = append(chunks, fmt.Sprintf(`<video src="%s"/>`, src))
		default:
			chunks = append(chunks, fmt.Sprintf(`<file src="%s"/>`, src))
		}
	}
	return strings.TrimSpace(strings.Join(chunks, " "))
}
