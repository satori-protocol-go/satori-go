package qq

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"github.com/WindowsSov8forUs/botgo-plus/dto"
	"github.com/satori-protocol-go/satori-go/pkg/satori/logging"
	satorievent "github.com/satori-protocol-go/satori-go/pkg/satori/model/event"
)

func (a *Adapter) logEventBySource(eventType dto.EventType, evt *satorievent.Event) {
	if evt == nil {
		return
	}

	switch eventType {
	case dto.EventMessageCreate, dto.EventAtMessageCreate:
		userName := eventUserDisplayName(evt)
		a.logf(logging.LevelInfo, "收到来自频道 %s 的子频道 %s 的用户 %s 的消息: %s",
			eventGuildID(evt), eventChannelID(evt), userName, eventMessageLogContent(evt))
	case dto.EventDirectMessageCreate:
		userName := eventUserDisplayName(evt)
		a.logf(logging.LevelInfo, "收到来自用户 %s 的私聊频道消息: %s", userName, eventMessageLogContent(evt))
	case dto.EventGroupAtMessageCreate:
		a.logf(logging.LevelInfo, "收到来自群 %s 用户 %s 的消息: %s",
			eventGuildOrChannelID(evt), eventUserID(evt), eventMessageLogContent(evt))
	case dto.EventC2CMessageCreate:
		a.logf(logging.LevelInfo, "收到来自用户 %s 的私聊消息: %s", eventUserID(evt), eventMessageLogContent(evt))

	case dto.EventGuildCreate:
		a.logf(logging.LevelInfo, "用户 %s 创建了频道 %s 。", eventOperatorID(evt), eventGuildName(evt))
	case dto.EventGuildUpdate:
		a.logf(logging.LevelInfo, "用户 %s 更新了频道 %s 的信息。", eventOperatorID(evt), eventGuildName(evt))
	case dto.EventGuildDelete:
		a.logf(logging.LevelInfo, "用户 %s 删除了频道 %s 。", eventOperatorID(evt), eventGuildName(evt))

	case dto.EventChannelCreate:
		a.logf(logging.LevelInfo, "用户 %s 在频道 %s 创建了 %s 。", eventOperatorID(evt), eventGuildID(evt), eventChannelName(evt))
	case dto.EventChannelUpdate:
		a.logf(logging.LevelInfo, "用户 %s 在频道 %s 更新了 %s 的信息。", eventOperatorID(evt), eventGuildID(evt), eventChannelName(evt))
	case dto.EventChannelDelete:
		a.logf(logging.LevelInfo, "用户 %s 在频道 %s 删除了 %s 。", eventOperatorID(evt), eventGuildID(evt), eventChannelName(evt))

	case dto.EventGuildMemberAdd:
		memberName := eventMemberDisplayName(evt)
		if eventUserID(evt) == eventOperatorID(evt) {
			a.logf(logging.LevelInfo, "用户 %s 加入了频道 %s 。", memberName, eventGuildID(evt))
		} else {
			a.logf(logging.LevelInfo, "用户 %s 邀请了用户 %s 加入频道 %s 。", eventOperatorID(evt), memberName, eventGuildID(evt))
		}
	case dto.EventGuildMemberUpdate:
		memberName := eventMemberDisplayName(evt)
		if eventUserID(evt) == eventOperatorID(evt) {
			a.logf(logging.LevelInfo, "频道 %s 的用户 %s 更新了自己的信息。", eventGuildID(evt), memberName)
		} else {
			a.logf(logging.LevelInfo, "频道 %s 的用户 %s 更新了用户 %s 的信息。", eventGuildID(evt), eventOperatorID(evt), memberName)
		}
	case dto.EventGuildMemberRemove, dto.EventType("GUILD_MEMBER_DELETE"):
		memberName := eventMemberDisplayName(evt)
		if eventUserID(evt) == eventOperatorID(evt) {
			a.logf(logging.LevelInfo, "用户 %s 离开了频道 %s 。", memberName, eventGuildID(evt))
		} else {
			a.logf(logging.LevelInfo, "用户 %s 将用户 %s 移出了频道 %s 。", eventOperatorID(evt), memberName, eventGuildID(evt))
		}

	case dto.EventMessageDelete, dto.EventPublicMessageDelete:
		operatorName := eventOperatorDisplayName(evt)
		memberName := eventMessageAuthorDisplayName(evt)
		if memberName == "" || eventOperatorID(evt) == eventUserID(evt) {
			a.logf(logging.LevelInfo, "频道 %s 的子频道 %s 的用户 %s 撤回了一条消息。", eventGuildID(evt), eventChannelID(evt), operatorName)
		} else {
			a.logf(logging.LevelInfo, "频道 %s 的子频道 %s 的用户 %s 撤回了用户 %s 的一条消息。", eventGuildID(evt), eventChannelID(evt), operatorName, memberName)
		}
	case dto.EventDirectMessageDelete:
		a.logf(logging.LevelInfo, "用户 %s 撤回了一条私聊频道消息。", eventOperatorDisplayName(evt))

	case dto.EventMessageReactionAdd:
		a.logf(logging.LevelInfo, "频道 %s 的子频道 %s 的用户 %s 对 %s 进行了表态: %s",
			eventGuildID(evt), eventChannelID(evt), eventUserID(evt), reactionTargetName(evt), reactionEmojiName(evt))
	case dto.EventMessageReactionRemove:
		a.logf(logging.LevelInfo, "频道 %s 的子频道 %s 的用户 %s 对 %s 移除了表态: %s",
			eventGuildID(evt), eventChannelID(evt), eventUserID(evt), reactionTargetName(evt), reactionEmojiName(evt))

	case dto.EventGroupAddRobot:
		a.logf(logging.LevelInfo, "机器人被 %s 添加进了群组 %s", eventOperatorID(evt), eventGuildOrChannelID(evt))
	case dto.EventGroupDelRobot:
		a.logf(logging.LevelInfo, "机器人被 %s 移出了群组 %s", eventOperatorID(evt), eventGuildOrChannelID(evt))
	}
}

func eventDataMap(evt *satorievent.Event) map[string]any {
	if evt == nil || evt.Data_ == nil {
		return nil
	}
	data, ok := evt.Data_.(map[string]any)
	if !ok {
		return nil
	}
	return data
}

func eventMessageLogContent(evt *satorievent.Event) string {
	data := eventDataMap(evt)
	if data != nil {
		if value := strings.TrimSpace(anyToString(data["content"])); value != "" {
			return value
		}
	}
	if evt != nil && evt.Message != nil {
		if value := strings.TrimSpace(evt.Message.Content); value != "" {
			return value
		}
	}
	return ""
}

func eventChannelName(evt *satorievent.Event) string {
	channelID := eventChannelID(evt)
	channelName := ""
	channelType := 0
	if evt != nil && evt.Channel != nil {
		channelName = strings.TrimSpace(evt.Channel.Name)
		channelType = int(evt.Channel.Type)
	}
	if data := eventDataMap(evt); data != nil {
		if rawType, ok := anyToInt(data["type"]); ok {
			channelType = rawType
		}
		if channelName == "" {
			channelName = strings.TrimSpace(anyToString(data["name"]))
		}
	}

	builder := channelTypeToString(channelType) + " "
	if channelName != "" {
		return builder + fmt.Sprintf("%s(%s)", channelName, channelID)
	}
	return builder + channelID
}

func eventGuildName(evt *satorievent.Event) string {
	guildID := eventGuildID(evt)
	if evt != nil && evt.Guild != nil && strings.TrimSpace(evt.Guild.Name) != "" {
		return fmt.Sprintf("%s(%s)", strings.TrimSpace(evt.Guild.Name), guildID)
	}
	return guildID
}

func eventUserDisplayName(evt *satorievent.Event) string {
	userID := eventUserID(evt)
	if evt != nil && evt.Member != nil && strings.TrimSpace(evt.Member.Nick) != "" {
		return fmt.Sprintf("%s(%s)", strings.TrimSpace(evt.Member.Nick), userID)
	}
	if evt != nil && evt.User != nil {
		if name := strings.TrimSpace(evt.User.Name); name != "" {
			return fmt.Sprintf("%s(%s)", name, userID)
		}
	}
	return userID
}

func eventMemberDisplayName(evt *satorievent.Event) string {
	userID := eventUserID(evt)
	if evt != nil && evt.Member != nil && strings.TrimSpace(evt.Member.Nick) != "" {
		return fmt.Sprintf("%s(%s)", strings.TrimSpace(evt.Member.Nick), userID)
	}
	if evt != nil && evt.User != nil && strings.TrimSpace(evt.User.Name) != "" {
		return fmt.Sprintf("%s(%s)", strings.TrimSpace(evt.User.Name), userID)
	}
	return userID
}

func eventOperatorDisplayName(evt *satorievent.Event) string {
	operatorID := eventOperatorID(evt)
	if evt != nil && evt.Operator != nil && strings.TrimSpace(evt.Operator.Name) != "" {
		return fmt.Sprintf("%s(%s)", strings.TrimSpace(evt.Operator.Name), operatorID)
	}
	return operatorID
}

func eventMessageAuthorDisplayName(evt *satorievent.Event) string {
	if evt == nil || evt.Message == nil {
		return ""
	}
	if evt.Message.Member != nil && strings.TrimSpace(evt.Message.Member.Nick) != "" && evt.Message.User != nil {
		return fmt.Sprintf("%s(%s)", strings.TrimSpace(evt.Message.Member.Nick), strings.TrimSpace(evt.Message.User.Id))
	}
	if evt.Message.User != nil {
		if name := strings.TrimSpace(evt.Message.User.Name); name != "" {
			return fmt.Sprintf("%s(%s)", name, strings.TrimSpace(evt.Message.User.Id))
		}
		return strings.TrimSpace(evt.Message.User.Id)
	}
	return ""
}

func reactionTargetName(evt *satorievent.Event) string {
	targetType := 0
	targetID := ""
	if data := eventDataMap(evt); data != nil {
		if target, ok := data["target"].(map[string]any); ok {
			if value, ok := anyToInt(target["type"]); ok {
				targetType = value
			}
			targetID = anyToString(target["id"])
		}
	}
	if targetID == "" && evt != nil && evt.Message != nil {
		targetID = strings.TrimSpace(evt.Message.Id)
	}
	return fmt.Sprintf("%s(%s)", reactionTargetTypeToString(targetType), targetID)
}

func reactionEmojiName(evt *satorievent.Event) string {
	emojiType := 0
	emojiID := ""
	if data := eventDataMap(evt); data != nil {
		if emoji, ok := data["emoji"].(map[string]any); ok {
			if value, ok := anyToInt(emoji["type"]); ok {
				emojiType = value
			}
			emojiID = anyToString(emoji["id"])
		}
	}
	return fmt.Sprintf("%s(%s)", reactionEmojiTypeToString(emojiType), emojiID)
}

func eventGuildID(evt *satorievent.Event) string {
	if evt != nil && evt.Guild != nil && strings.TrimSpace(evt.Guild.Id) != "" {
		return strings.TrimSpace(evt.Guild.Id)
	}
	if data := eventDataMap(evt); data != nil {
		if guildID := strings.TrimSpace(anyToString(data["guild_id"])); guildID != "" {
			return guildID
		}
	}
	return ""
}

func eventChannelID(evt *satorievent.Event) string {
	if evt != nil && evt.Channel != nil && strings.TrimSpace(evt.Channel.Id) != "" {
		return strings.TrimSpace(evt.Channel.Id)
	}
	if data := eventDataMap(evt); data != nil {
		if channelID := strings.TrimSpace(anyToString(data["channel_id"])); channelID != "" {
			return channelID
		}
	}
	return ""
}

func eventGuildOrChannelID(evt *satorievent.Event) string {
	if guildID := eventGuildID(evt); guildID != "" {
		return guildID
	}
	return eventChannelID(evt)
}

func eventUserID(evt *satorievent.Event) string {
	if evt != nil && evt.User != nil && strings.TrimSpace(evt.User.Id) != "" {
		return strings.TrimSpace(evt.User.Id)
	}
	if evt != nil && evt.Member != nil && evt.Member.User != nil && strings.TrimSpace(evt.Member.User.Id) != "" {
		return strings.TrimSpace(evt.Member.User.Id)
	}
	if evt != nil && evt.Message != nil && evt.Message.User != nil && strings.TrimSpace(evt.Message.User.Id) != "" {
		return strings.TrimSpace(evt.Message.User.Id)
	}
	if data := eventDataMap(evt); data != nil {
		if userID := strings.TrimSpace(anyToString(data["user_id"])); userID != "" {
			return userID
		}
		if author, ok := data["author"].(map[string]any); ok {
			if userID := strings.TrimSpace(anyToString(author["id"])); userID != "" {
				return userID
			}
			if userID := strings.TrimSpace(anyToString(author["user_openid"])); userID != "" {
				return userID
			}
			if userID := strings.TrimSpace(anyToString(author["member_openid"])); userID != "" {
				return userID
			}
		}
	}
	return ""
}

func eventOperatorID(evt *satorievent.Event) string {
	if evt != nil && evt.Operator != nil && strings.TrimSpace(evt.Operator.Id) != "" {
		return strings.TrimSpace(evt.Operator.Id)
	}
	if data := eventDataMap(evt); data != nil {
		if operatorID := strings.TrimSpace(anyToString(data["op_user_id"])); operatorID != "" {
			return operatorID
		}
		if operatorID := strings.TrimSpace(anyToString(data["op_member_openid"])); operatorID != "" {
			return operatorID
		}
	}
	return ""
}

func channelTypeToString(channelType int) string {
	switch dto.ChannelType(channelType) {
	case dto.ChannelTypeText:
		return "文字子频道"
	case dto.ChannelTypeVoice:
		return "语音子频道"
	case dto.ChannelTypeCategory:
		return "子频道分组"
	case dto.ChannelTypeLive:
		return "直播子频道"
	case dto.ChannelTypeApplication:
		return "应用子频道"
	case dto.ChannelTypeForum:
		return "论坛子频道"
	default:
		return "未知类型子频道"
	}
}

func reactionTargetTypeToString(targetType int) string {
	switch dto.ReactionTargetType(targetType) {
	case dto.ReactionTargetTypeMsg:
		return "消息"
	case dto.ReactionTargetTypeFeed:
		return "帖子"
	case dto.ReactionTargetTypeComment:
		return "评论"
	case dto.ReactionTargetTypeReply:
		return "回复"
	default:
		return "[" + strconv.Itoa(targetType) + "]"
	}
}

func reactionEmojiTypeToString(emojiType int) string {
	switch emojiType {
	case 1:
		return "系统表情"
	case 2:
		return "emoji表情"
	default:
		return "[表情" + strconv.Itoa(emojiType) + "]"
	}
}

func anyToString(value any) string {
	switch typed := value.(type) {
	case string:
		return typed
	case json.Number:
		return typed.String()
	default:
		return fmt.Sprint(value)
	}
}

func anyToInt(value any) (int, bool) {
	switch typed := value.(type) {
	case int:
		return typed, true
	case int8:
		return int(typed), true
	case int16:
		return int(typed), true
	case int32:
		return int(typed), true
	case int64:
		return int(typed), true
	case uint:
		return int(typed), true
	case uint8:
		return int(typed), true
	case uint16:
		return int(typed), true
	case uint32:
		return int(typed), true
	case uint64:
		return int(typed), true
	case float32:
		return int(typed), true
	case float64:
		return int(typed), true
	case json.Number:
		number, err := typed.Int64()
		if err == nil {
			return int(number), true
		}
		f, err := typed.Float64()
		if err != nil {
			return 0, false
		}
		return int(f), true
	case string:
		text := strings.TrimSpace(typed)
		if text == "" {
			return 0, false
		}
		number, err := strconv.Atoi(text)
		if err != nil {
			return 0, false
		}
		return number, true
	default:
		return 0, false
	}
}
