package action

import (
	botgodto "github.com/WindowsSov8forUs/botgo-plus/dto"
	qqcodec "github.com/satori-protocol-go/satori-go/pkg/satori/adapter/qq/codec"
	"github.com/satori-protocol-go/satori-go/pkg/satori/model/define"
	"github.com/satori-protocol-go/satori-go/pkg/satori/model/user"
	satoriserver "github.com/satori-protocol-go/satori-go/pkg/satori/server"
)

func (h *Handler) handleReactionList(request satoriserver.Request[satoriserver.ReactionListParam]) (any, error) {
	if request.Platform != "qqguild" {
		return nil, satoriserver.NotFound("reaction.list is not supported in current platform")
	}
	api, ok := h.apiV1.(apiReactionActions)
	if !ok {
		return nil, satoriserver.NotFound("reaction.list capability is unavailable")
	}
	channelID, err := requiredString(request.Params.ChannelID, "channel_id")
	if err != nil {
		return nil, err
	}
	messageID, err := requiredString(request.Params.MessageID, "message_id")
	if err != nil {
		return nil, err
	}
	emojiRaw, err := requiredString(request.Params.Emoji, "emoji")
	if err != nil {
		return nil, err
	}

	pager := &botgodto.MessageReactionPager{Limit: "50"}
	if next := optionalString(request.Params.Next); next != "" {
		pager.Cookie = next
	}

	items, err := api.GetMessageReactionUsers(
		requestContext(request.Origin),
		splitChannelCompositeID(channelID),
		messageID,
		qqcodec.ParseReactionEmoji(emojiRaw),
		pager,
	)
	if err != nil {
		return nil, err
	}
	if items == nil {
		return &define.Paginated[*user.User]{Data: []*user.User{}}, nil
	}

	response := &define.Paginated[*user.User]{Data: qqcodec.ReactionUsersFromDTO(items.Users)}
	if !items.IsEnd {
		response.Next = items.Cookie
	}
	return response, nil
}

func (h *Handler) handleReactionCreate(request satoriserver.Request[satoriserver.ReactionCreateParam]) (any, error) {
	if request.Platform != "qqguild" {
		return nil, satoriserver.NotFound("reaction.create is not supported in current platform")
	}
	api, ok := h.apiV1.(apiReactionActions)
	if !ok {
		return nil, satoriserver.NotFound("reaction.create capability is unavailable")
	}
	channelID, err := requiredString(request.Params.ChannelID, "channel_id")
	if err != nil {
		return nil, err
	}
	messageID, err := requiredString(request.Params.MessageID, "message_id")
	if err != nil {
		return nil, err
	}
	emojiRaw, err := requiredString(request.Params.Emoji, "emoji")
	if err != nil {
		return nil, err
	}

	if err := api.CreateMessageReaction(
		requestContext(request.Origin),
		splitChannelCompositeID(channelID),
		messageID,
		qqcodec.ParseReactionEmoji(emojiRaw),
	); err != nil {
		return nil, err
	}
	return map[string]any{}, nil
}

func (h *Handler) handleReactionDelete(request satoriserver.Request[satoriserver.ReactionDeleteParam]) (any, error) {
	if request.Platform != "qqguild" {
		return nil, satoriserver.NotFound("reaction.delete is not supported in current platform")
	}
	api, ok := h.apiV1.(apiReactionActions)
	if !ok {
		return nil, satoriserver.NotFound("reaction.delete capability is unavailable")
	}
	channelID, err := requiredString(request.Params.ChannelID, "channel_id")
	if err != nil {
		return nil, err
	}
	messageID, err := requiredString(request.Params.MessageID, "message_id")
	if err != nil {
		return nil, err
	}
	emojiRaw, err := requiredString(request.Params.Emoji, "emoji")
	if err != nil {
		return nil, err
	}

	if err := api.DeleteOwnMessageReaction(
		requestContext(request.Origin),
		splitChannelCompositeID(channelID),
		messageID,
		qqcodec.ParseReactionEmoji(emojiRaw),
	); err != nil {
		return nil, err
	}
	return map[string]any{}, nil
}

func (h *Handler) handleReactionClear(request satoriserver.Request[satoriserver.ReactionClearParam]) (any, error) {
	_ = request
	return nil, satoriserver.NotFound("reaction.clear is not supported")
}
