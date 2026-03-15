package action

import (
	"strings"

	botgodto "github.com/WindowsSov8forUs/botgo-plus/dto"
	qqcodec "github.com/satori-protocol-go/satori-go/pkg/satori/adapter/qq/codec"
	"github.com/satori-protocol-go/satori-go/pkg/satori/model/define"
	"github.com/satori-protocol-go/satori-go/pkg/satori/model/guild"
	satoriserver "github.com/satori-protocol-go/satori-go/pkg/satori/server"
)

func (h *Handler) handleGuildGet(request satoriserver.Request[satoriserver.GuildGetParam]) (any, error) {
	if request.Platform != "qqguild" {
		return nil, satoriserver.NotFound("guild.get is not supported in current platform")
	}
	api, ok := h.apiV1.(apiGuildActions)
	if !ok {
		return nil, satoriserver.NotFound("guild.get capability is unavailable")
	}
	guildID, err := requiredString(request.Params.GuildID, "guild_id")
	if err != nil {
		return nil, err
	}

	fetched, err := api.Guild(requestContext(request.Origin), splitGuildCompositeID(guildID))
	if err != nil {
		return nil, err
	}
	if fetched == nil {
		return nil, satoriserver.NotFound("guild not found")
	}
	return qqcodec.GuildFromDTO(fetched), nil
}

func (h *Handler) handleGuildList(request satoriserver.Request[satoriserver.GuildListParam]) (any, error) {
	if request.Platform != "qqguild" {
		return nil, satoriserver.NotFound("guild.list is not supported in current platform")
	}
	api, ok := h.apiV1.(apiGuildActions)
	if !ok {
		return nil, satoriserver.NotFound("guild.list capability is unavailable")
	}
	pager := &botgodto.GuildPager{Limit: "100"}
	if nextValue, ok := request.Params.Next.Get(); ok {
		next := strings.TrimSpace(nextValue)
		if next != "" {
			pager.After = next
		}
	}

	items, err := api.MeGuilds(requestContext(request.Origin), pager)
	if err != nil {
		return nil, err
	}
	data := make([]*guild.Guild, 0, len(items))
	for _, item := range items {
		data = append(data, qqcodec.GuildFromDTO(item))
	}

	response := &define.Paginated[*guild.Guild]{Data: data}
	if len(data) > 0 {
		response.Next = data[len(data)-1].Id
	}
	return response, nil
}
