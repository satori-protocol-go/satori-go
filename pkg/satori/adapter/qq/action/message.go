package action

import (
	"errors"
	"strconv"
	"strings"

	botgodto "github.com/WindowsSov8forUs/botgo-plus/dto"
	qqcodec "github.com/satori-protocol-go/satori-go/pkg/satori/adapter/qq/codec"
	qqmessagesend "github.com/satori-protocol-go/satori-go/pkg/satori/adapter/qq/messagesend"
	"github.com/satori-protocol-go/satori-go/pkg/satori/model/define"
	"github.com/satori-protocol-go/satori-go/pkg/satori/model/message"
	satoriserver "github.com/satori-protocol-go/satori-go/pkg/satori/server"
)

func (h *Handler) handleMessageCreate(request satoriserver.Request[satoriserver.MessageCreateParam]) (any, error) {
	channelID, err := requiredString(request.Params.ChannelID, "channel_id")
	if err != nil {
		return nil, err
	}
	content := request.Params.Content

	if h.sender == nil {
		return []*message.Message{}, nil
	}
	result, err := h.sender.Send(requestContext(request.Origin), qqmessagesend.CreateInput{
		Platform:  request.Platform,
		ChannelID: channelID,
		Content:   content,
		Referrer:  parseMessageReferrer(request.Params.Referrer),
	})
	if err != nil {
		if errors.Is(err, qqmessagesend.ErrUnsupportedPlatform) {
			return nil, satoriserver.NotFound("unsupported platform")
		}
		return nil, err
	}
	if result == nil {
		return []*message.Message{}, nil
	}
	return result, nil
}

func (h *Handler) handleMessageUpdate(request satoriserver.Request[satoriserver.MessageUpdateParam]) (any, error) {
	if request.Platform != "qqguild" {
		return nil, satoriserver.NotFound("message.update is not supported in current platform")
	}
	channelID, err := requiredString(request.Params.ChannelID, "channel_id")
	if err != nil {
		return nil, err
	}
	if strings.Contains(channelID, "_") {
		return nil, satoriserver.NotFound("message.update is not supported for user-channel")
	}
	messageID, err := requiredString(request.Params.MessageID, "message_id")
	if err != nil {
		return nil, err
	}

	api, ok := h.apiV1.(apiMessageAdvanced)
	if !ok {
		return nil, satoriserver.NotFound("message.update capability is unavailable")
	}

	payload := &botgodto.MessageToCreate{Content: optionalString(request.Params.Content)}
	updated, err := api.PatchMessage(requestContext(request.Origin), channelID, messageID, payload)
	if err != nil {
		return nil, err
	}
	if updated == nil {
		return map[string]any{}, nil
	}
	return qqcodec.MessageFromDTO(updated, request.Platform), nil
}

func (h *Handler) handleMessageDelete(request satoriserver.Request[satoriserver.MessageOpParam]) (any, error) {
	channelID, err := requiredString(request.Params.ChannelID, "channel_id")
	if err != nil {
		return nil, err
	}
	messageID, err := requiredString(request.Params.MessageID, "message_id")
	if err != nil {
		return nil, err
	}

	ctx := requestContext(request.Origin)
	switch request.Platform {
	case "qqguild":
		if strings.Contains(channelID, "_") {
			parts := strings.SplitN(channelID, "_", 2)
			err = h.apiV1.RetractDMMessage(ctx, parts[0], messageID)
		} else {
			err = h.apiV1.RetractMessage(ctx, channelID, messageID)
		}
	case "qq":
		if strings.HasPrefix(channelID, "private:") {
			userID := strings.TrimPrefix(channelID, "private:")
			err = h.apiV2.RetractC2CMessage(ctx, userID, messageID)
		} else {
			err = h.apiV2.RetractGroupMessage(ctx, channelID, messageID)
		}
	default:
		return nil, satoriserver.NotFound("unsupported platform")
	}
	if err != nil {
		return nil, err
	}
	return map[string]any{}, nil
}

func (h *Handler) handleMessageGet(request satoriserver.Request[satoriserver.MessageOpParam]) (any, error) {
	if request.Platform != "qqguild" {
		return nil, satoriserver.NotFound("message.get is not supported in current platform")
	}
	channelID, err := requiredString(request.Params.ChannelID, "channel_id")
	if err != nil {
		return nil, err
	}
	if strings.Contains(channelID, "_") {
		return nil, satoriserver.NotFound("user-channel is not supported for message.get")
	}
	messageID, err := requiredString(request.Params.MessageID, "message_id")
	if err != nil {
		return nil, err
	}

	fetched, err := h.apiV1.Message(requestContext(request.Origin), channelID, messageID)
	if err != nil {
		return nil, err
	}
	if fetched == nil {
		return nil, satoriserver.NotFound("message not found")
	}
	return qqcodec.MessageFromDTO(fetched, request.Platform), nil
}

func (h *Handler) handleMessageList(request satoriserver.Request[satoriserver.MessageListParam]) (any, error) {
	if request.Platform != "qqguild" {
		return nil, satoriserver.NotFound("message.list is not supported in current platform")
	}
	channelID, err := requiredString(request.Params.ChannelID, "channel_id")
	if err != nil {
		return nil, err
	}
	if strings.Contains(channelID, "_") {
		return nil, satoriserver.NotFound("message.list is not supported for user-channel")
	}

	api, ok := h.apiV1.(apiMessageAdvanced)
	if !ok {
		return nil, satoriserver.NotFound("message.list capability is unavailable")
	}

	pager := &botgodto.MessagesPager{Limit: "20"}
	if limit, ok := optionalInt(request.Params.Limit); ok && limit > 0 {
		pager.Limit = strconv.Itoa(limit)
	}
	if next := optionalString(request.Params.Next); next != "" {
		switch strings.ToLower(optionalString(request.Params.Direction)) {
		case "after":
			pager.Type = botgodto.MPTAfter
		default:
			pager.Type = botgodto.MPTBefore
		}
		pager.ID = next
	}
	if prev := optionalString(request.Params.Prev); prev != "" {
		pager.Type = botgodto.MPTAfter
		pager.ID = prev
	}

	items, err := api.Messages(requestContext(request.Origin), channelID, pager)
	if err != nil {
		return nil, err
	}
	result := make([]*message.Message, 0, len(items))
	for _, item := range items {
		result = append(result, qqcodec.MessageFromDTO(item, request.Platform))
	}

	response := &define.BidiPaginated[*message.Message]{
		Data: result,
	}
	if len(result) > 0 {
		response.Prev = result[0].Id
		response.Next = result[len(result)-1].Id
	}
	return response, nil
}

func parseMessageReferrer(raw *satoriserver.MessageReferrerParam) qqmessagesend.Referrer {
	result := qqmessagesend.Referrer{}
	if raw == nil {
		return result
	}
	result.MsgID = optionalString(raw.MsgID)
	result.Direct = raw.Direct
	if seq, ok := optionalInt(raw.MsgSeq); ok {
		result.MsgSeq = seq
		result.HasMsgSeq = true
	}
	return result
}
