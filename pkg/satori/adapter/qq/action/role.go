package action

import (
	botgodto "github.com/WindowsSov8forUs/botgo-plus/dto"
	qqcodec "github.com/satori-protocol-go/satori-go/pkg/satori/adapter/qq/codec"
	"github.com/satori-protocol-go/satori-go/pkg/satori/model/define"
	"github.com/satori-protocol-go/satori-go/pkg/satori/model/guildrole"
	satoriserver "github.com/satori-protocol-go/satori-go/pkg/satori/server"
)

func (h *Handler) handleGuildRoleList(request satoriserver.Request[satoriserver.GuildListByGuildParam]) (any, error) {
	if request.Platform != "qqguild" {
		return nil, satoriserver.NotFound("guild.role.list is not supported in current platform")
	}
	api, ok := h.apiV1.(apiRoleActions)
	if !ok {
		return nil, satoriserver.NotFound("guild.role.list capability is unavailable")
	}
	guildID, err := requiredString(request.Params.GuildID, "guild_id")
	if err != nil {
		return nil, err
	}

	roles, err := api.Roles(requestContext(request.Origin), splitGuildCompositeID(guildID))
	if err != nil {
		return nil, err
	}
	if roles == nil {
		return &define.Paginated[*guildrole.GuildRole]{Data: []*guildrole.GuildRole{}}, nil
	}
	return &define.Paginated[*guildrole.GuildRole]{Data: qqcodec.RolesFromDTO(roles.Roles)}, nil
}

func (h *Handler) handleGuildRoleCreate(request satoriserver.Request[satoriserver.GuildRoleCreateParam]) (any, error) {
	if request.Platform != "qqguild" {
		return nil, satoriserver.NotFound("guild.role.create is not supported in current platform")
	}
	api, ok := h.apiV1.(apiRoleActions)
	if !ok {
		return nil, satoriserver.NotFound("guild.role.create capability is unavailable")
	}
	guildID, err := requiredString(request.Params.GuildID, "guild_id")
	if err != nil {
		return nil, err
	}
	if request.Params.Role == nil {
		return nil, satoriserver.BadRequest("role is required")
	}

	updated, err := api.PostRole(
		requestContext(request.Origin),
		splitGuildCompositeID(guildID),
		qqcodec.ParseRole(request.Params.Role),
	)
	if err != nil {
		return nil, err
	}
	if updated == nil || updated.Role == nil {
		return nil, satoriserver.NotFound("role not created")
	}
	items := qqcodec.RolesFromDTO([]*botgodto.Role{updated.Role})
	if len(items) == 0 {
		return nil, satoriserver.NotFound("role not created")
	}
	return items[0], nil
}

func (h *Handler) handleGuildRoleUpdate(request satoriserver.Request[satoriserver.GuildRoleUpdateParam]) (any, error) {
	if request.Platform != "qqguild" {
		return nil, satoriserver.NotFound("guild.role.update is not supported in current platform")
	}
	api, ok := h.apiV1.(apiRoleActions)
	if !ok {
		return nil, satoriserver.NotFound("guild.role.update capability is unavailable")
	}
	guildID, err := requiredString(request.Params.GuildID, "guild_id")
	if err != nil {
		return nil, err
	}
	roleID, err := requiredString(request.Params.RoleID, "role_id")
	if err != nil {
		return nil, err
	}
	if request.Params.Role == nil {
		return nil, satoriserver.BadRequest("role is required")
	}

	updated, err := api.PatchRole(
		requestContext(request.Origin),
		splitGuildCompositeID(guildID),
		botgodto.RoleID(roleID),
		qqcodec.ParseRole(request.Params.Role),
	)
	if err != nil {
		return nil, err
	}
	if updated == nil || updated.Role == nil {
		return map[string]any{}, nil
	}
	items := qqcodec.RolesFromDTO([]*botgodto.Role{updated.Role})
	if len(items) == 0 {
		return map[string]any{}, nil
	}
	return items[0], nil
}

func (h *Handler) handleGuildRoleDelete(request satoriserver.Request[satoriserver.GuildRoleDeleteParam]) (any, error) {
	if request.Platform != "qqguild" {
		return nil, satoriserver.NotFound("guild.role.delete is not supported in current platform")
	}
	api, ok := h.apiV1.(apiRoleActions)
	if !ok {
		return nil, satoriserver.NotFound("guild.role.delete capability is unavailable")
	}
	guildID, err := requiredString(request.Params.GuildID, "guild_id")
	if err != nil {
		return nil, err
	}
	roleID, err := requiredString(request.Params.RoleID, "role_id")
	if err != nil {
		return nil, err
	}

	if err := api.DeleteRole(requestContext(request.Origin), splitGuildCompositeID(guildID), botgodto.RoleID(roleID)); err != nil {
		return nil, err
	}
	return map[string]any{}, nil
}
