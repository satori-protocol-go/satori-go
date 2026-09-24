package convert

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"

	"github.com/WindowsSov8forUs/botgo-plus/dto"
	"github.com/satori-protocol-go/satori-go/pkg/satori/internal/xhtml"
	"github.com/satori-protocol-go/satori-go/pkg/satori/model/channel"
	"github.com/satori-protocol-go/satori-go/pkg/satori/model/guild"
	"github.com/satori-protocol-go/satori-go/pkg/satori/model/message"
)

var nativeTextToken = regexp.MustCompile(`<@!?(\w+)>|<#(\w+)>|<emoji:(\w+)>`)

func MessageFromDTO(input *dto.Message, platform string) *message.Message {
	if input == nil {
		return nil
	}
	result := &message.Message{Id: input.ID, Content: messageContentFromDTO(input), Referrer: map[string]any{"msg_id": input.ID, "msg_seq": -1}}
	if created, err := input.Timestamp.Time(); err == nil {
		result.CreateAt = created.UnixMilli()
	}
	if updated, err := input.EditedTimestamp.Time(); err == nil {
		result.UpdateAt = updated.UnixMilli()
	}
	result.User = UserFromDTO(input.Author)
	result.Member = MemberFromDTO(input.Member)
	groupID := input.EffectiveGroupID()
	if result.User != nil && input.Author != nil && platform == "qq" {
		if groupID != "" && input.Author.MemberOpenID != "" {
			result.User.Id = input.Author.MemberOpenID
		}
		if groupID == "" && input.Author.UserOpenID != "" {
			result.User.Id = input.Author.UserOpenID
		}
	}
	switch {
	case input.ChannelID != "":
		kind := channel.ChannelTypeText
		if input.DirectMessage {
			kind = channel.ChannelTypeDirect
			result.Referrer["direct"] = true
		}
		result.Channel = &channel.Channel{Id: input.ChannelID, Type: kind}
		if input.GuildID != "" {
			result.Guild = &guild.Guild{Id: input.GuildID}
		}
	case groupID != "":
		result.Channel = &channel.Channel{Id: groupID, Type: channel.ChannelTypeText}
		result.Guild = &guild.Guild{Id: groupID}
	case result.User != nil && result.User.Id != "":
		result.Channel = &channel.Channel{Id: "private:" + result.User.Id, Type: channel.ChannelTypeDirect}
		result.Referrer["direct"] = true
	}
	if len(input.MessageScene.Ext) > 0 || input.MessageScene.Source != "" || input.MessageScene.CallbackData != "" {
		result.Referrer["msg_scene"] = input.MessageScene
	}
	if index, ok := input.MessageScene.GetExt("msg_idx"); ok {
		result.Referrer["ref_idx"] = index
	}
	if input.ExtInfo != nil && input.ExtInfo.RefIdx != "" {
		result.Referrer["ref_idx"] = input.ExtInfo.RefIdx
	}
	return result
}

// Only native mention/emoji tokens become elements. Literal user content is
// escaped so an incoming '<img>' cannot turn into an executable resource request.
func nativeText(content string) string {
	var out strings.Builder
	position := 0
	for _, match := range nativeTextToken.FindAllStringSubmatchIndex(content, -1) {
		out.WriteString(xhtml.Escape(content[position:match[0]], false))
		switch {
		case match[2] >= 0:
			out.WriteString(xhtml.NewElement("at", map[string]any{"id": content[match[2]:match[3]]}).String())
		case match[4] >= 0:
			out.WriteString(xhtml.NewElement("sharp", map[string]any{"id": content[match[4]:match[5]]}).String())
		case match[6] >= 0:
			out.WriteString(xhtml.NewElement("qq:emoji", map[string]any{"id": content[match[6]:match[7]]}).String())
		}
		position = match[1]
	}
	out.WriteString(xhtml.Escape(content[position:], false))
	return out.String()
}

func messageContentFromDTO(input *dto.Message) string {
	if input == nil {
		return ""
	}
	var chunks []string
	if input.MentionEveryone {
		chunks = append(chunks, `<at type="all"/>`)
	}
	if input.MessageReference != nil && input.MessageReference.MessageID != "" {
		chunks = append(chunks, xhtml.NewElement("quote", map[string]any{"id": input.MessageReference.MessageID}).String())
	}
	chunks = append(chunks, nativeText(input.Content))
	for _, attachment := range input.Attachments {
		chunks = append(chunks, attachmentElement(attachment))
	}
	for _, embed := range input.Embeds {
		if embed == nil {
			continue
		}
		if raw, err := json.Marshal(embed); err == nil {
			chunks = append(chunks, xhtml.NewElement("qq:embed", map[string]any{"data": string(raw)}).String())
		}
	}
	if input.Ark != nil {
		if raw, err := json.Marshal(input.Ark); err == nil {
			chunks = append(chunks, "<qq:ark>"+xhtml.Escape(string(raw), false)+"</qq:ark>")
		}
	}
	if input.ArkData != nil {
		if raw, err := json.Marshal(input.ArkData); err == nil {
			chunks = append(chunks, xhtml.NewElement("qq:ark-data", map[string]any{"data": string(raw)}).String())
		}
	}
	if len(input.MsgElements) > 0 {
		chunks = append(chunks, nestedMessages(input.MsgElements))
	}
	return strings.Join(chunks, "")
}

func attachmentElement(item *dto.MessageAttachment) string {
	if item == nil {
		return ""
	}
	src := item.URL
	if src == "" {
		src = item.VoiceWAVURL
	}
	if src == "" {
		return ""
	}
	if !strings.HasPrefix(src, "http://") && !strings.HasPrefix(src, "https://") {
		src = "https://" + strings.TrimPrefix(src, "//")
	}
	kind := "file"
	switch {
	case strings.HasPrefix(item.ContentType, "image"):
		kind = "img"
	case strings.HasPrefix(item.ContentType, "audio") || item.ContentType == "voice":
		kind = "audio"
	case strings.HasPrefix(item.ContentType, "video"):
		kind = "video"
	}
	attrs := map[string]any{"src": src}
	if item.FileName != "" {
		attrs["title"] = item.FileName
	}
	if item.Duration > 0 {
		attrs["duration"] = item.Duration
	}
	if item.Width > 0 {
		attrs["width"] = item.Width
	}
	if item.Height > 0 {
		attrs["height"] = item.Height
	}
	if item.VoiceWAVURL != "" {
		attrs["qq:voice-wav-url"] = item.VoiceWAVURL
	}
	if item.ASRReferText != "" {
		attrs["qq:asr-text"] = item.ASRReferText
	}
	return xhtml.NewElement(kind, attrs).String()
}

func nestedMessages(items []dto.MessageElement) string {
	var out strings.Builder
	out.WriteString("<message forward>")
	for _, item := range items {
		attrs := ""
		if item.MsgIdx != "" {
			attrs = fmt.Sprintf(` id="%s"`, xhtml.Escape(item.MsgIdx, true))
		}
		out.WriteString("<message" + attrs + ">")
		if author := UserFromDTO(item.Author); author != nil {
			out.WriteString(xhtml.NewElement("author", map[string]any{"id": author.Id, "name": author.Name}).String())
		}
		out.WriteString(messageContentFromDTO(&dto.Message{Content: item.Content, Attachments: item.Attachments, ArkData: item.ArkData, MsgElements: item.MsgElements}))
		out.WriteString("</message>")
	}
	out.WriteString("</message>")
	return out.String()
}
