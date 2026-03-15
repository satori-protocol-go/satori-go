package action

import (
	qqcodec "github.com/satori-protocol-go/satori-go/pkg/satori/adapter/qq/codec"
	"github.com/satori-protocol-go/satori-go/pkg/satori/model/channel"
	"github.com/satori-protocol-go/satori-go/pkg/satori/model/define"
	satoriserver "github.com/satori-protocol-go/satori-go/pkg/satori/server"
)

func (h *Handler) handleChannelGet(request satoriserver.Request[satoriserver.ChannelParam]) (any, error) {
	if request.Platform != "qqguild" {
		return nil, satoriserver.NotFound("channel.get is not supported in current platform")
	}
	api, ok := h.apiV1.(apiChannelActions)
	if !ok {
		return nil, satoriserver.NotFound("channel.get capability is unavailable")
	}
	channelID, err := requiredString(request.Params.ChannelID, "channel_id")
	if err != nil {
		return nil, err
	}

	fetched, err := api.Channel(requestContext(request.Origin), splitChannelCompositeID(channelID))
	if err != nil {
		return nil, err
	}
	if fetched == nil {
		return nil, satoriserver.NotFound("channel not found")
	}
	return qqcodec.ChannelFromDTO(fetched), nil
}

func (h *Handler) handleChannelList(request satoriserver.Request[satoriserver.ChannelListParam]) (any, error) {
	if request.Platform != "qqguild" {
		return nil, satoriserver.NotFound("channel.list is not supported in current platform")
	}
	api, ok := h.apiV1.(apiChannelActions)
	if !ok {
		return nil, satoriserver.NotFound("channel.list capability is unavailable")
	}
	guildID, err := requiredString(request.Params.GuildID, "guild_id")
	if err != nil {
		return nil, err
	}

	channelsValue, err := api.Channels(requestContext(request.Origin), splitGuildCompositeID(guildID))
	if err != nil {
		return nil, err
	}
	data := make([]*channel.Channel, 0, len(channelsValue))
	for _, item := range channelsValue {
		data = append(data, qqcodec.ChannelFromDTO(item))
	}
	return &define.Paginated[*channel.Channel]{Data: data}, nil
}

func (h *Handler) handleChannelCreate(request satoriserver.Request[satoriserver.ChannelCreateParam]) (any, error) {
	if request.Platform != "qqguild" {
		return nil, satoriserver.NotFound("channel.create is not supported in current platform")
	}
	api, ok := h.apiV1.(apiChannelActions)
	if !ok {
		return nil, satoriserver.NotFound("channel.create capability is unavailable")
	}
	guildID, err := requiredString(request.Params.GuildID, "guild_id")
	if err != nil {
		return nil, err
	}
	if request.Params.Data == nil {
		return nil, satoriserver.BadRequest("data is required")
	}

	created, err := api.PostChannel(
		requestContext(request.Origin),
		splitGuildCompositeID(guildID),
		qqcodec.ParseChannelValue(request.Params.Data),
	)
	if err != nil {
		return nil, err
	}
	if created == nil {
		return nil, satoriserver.NotFound("channel not created")
	}
	return qqcodec.ChannelFromDTO(created), nil
}

func (h *Handler) handleChannelUpdate(request satoriserver.Request[satoriserver.ChannelUpdateParam]) (any, error) {
	if request.Platform != "qqguild" {
		return nil, satoriserver.NotFound("channel.update is not supported in current platform")
	}
	api, ok := h.apiV1.(apiChannelActions)
	if !ok {
		return nil, satoriserver.NotFound("channel.update capability is unavailable")
	}
	channelID, err := requiredString(request.Params.ChannelID, "channel_id")
	if err != nil {
		return nil, err
	}
	if request.Params.Data == nil {
		return nil, satoriserver.BadRequest("data is required")
	}

	updated, err := api.PatchChannel(
		requestContext(request.Origin),
		splitChannelCompositeID(channelID),
		qqcodec.ParseChannelValue(request.Params.Data),
	)
	if err != nil {
		return nil, err
	}
	if updated == nil {
		return map[string]any{}, nil
	}
	return qqcodec.ChannelFromDTO(updated), nil
}

func (h *Handler) handleChannelDelete(request satoriserver.Request[satoriserver.ChannelParam]) (any, error) {
	if request.Platform != "qqguild" {
		return nil, satoriserver.NotFound("channel.delete is not supported in current platform")
	}
	api, ok := h.apiV1.(apiChannelActions)
	if !ok {
		return nil, satoriserver.NotFound("channel.delete capability is unavailable")
	}
	channelID, err := requiredString(request.Params.ChannelID, "channel_id")
	if err != nil {
		return nil, err
	}

	if err := api.DeleteChannel(requestContext(request.Origin), splitChannelCompositeID(channelID)); err != nil {
		return nil, err
	}
	return map[string]any{}, nil
}

func (h *Handler) handleChannelMute(request satoriserver.Request[satoriserver.ChannelMuteParam]) (any, error) {
	_ = request
	return nil, satoriserver.NotFound("channel.mute is not supported")
}
