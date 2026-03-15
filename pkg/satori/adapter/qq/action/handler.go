package action

import (
	"context"

	qqcodec "github.com/satori-protocol-go/satori-go/pkg/satori/adapter/qq/codec"
	qqmessagesend "github.com/satori-protocol-go/satori-go/pkg/satori/adapter/qq/messagesend"
	"github.com/satori-protocol-go/satori-go/pkg/satori/model/login"
	satoriserver "github.com/satori-protocol-go/satori-go/pkg/satori/server"
)

type Router interface {
	Route(action string, call satoriserver.RouteCall) string
}

type Handler struct {
	apiV1 APIV1
	apiV2 APIV2

	ensureLogins   func(context.Context) error
	findLogin      func(platform string, selfID string) *login.Login
	handleInternal func(
		request satoriserver.Request[map[string]any],
		path string,
	) (*satoriserver.Response, error)

	sender *qqmessagesend.Sender
}

func New(deps Dependencies) *Handler {
	handler := &Handler{
		apiV1:          deps.APIV1,
		apiV2:          deps.APIV2,
		ensureLogins:   deps.EnsureLogins,
		handleInternal: deps.HandleInternal,
		sender:         deps.MessageSender,
	}
	if deps.FindLogin != nil {
		handler.findLogin = deps.FindLogin
	}

	if handler.sender == nil {
		var multipart qqmessagesend.GuildMultipartAPI
		if casted, ok := deps.APIV1.(apiMessageMultipart); ok {
			multipart = casted
		}
		handler.sender = qqmessagesend.New(qqmessagesend.Dependencies{
			GuildAPI:          deps.APIV1,
			GuildMultipartAPI: multipart,
			QQAPI:             deps.APIV2,
			ConvertMessage:    qqcodec.MessageFromDTO,
		})
	}
	return handler
}

func (h *Handler) Register(router Router) {
	router.Route(string(satoriserver.ApiLoginGet), satoriserver.WrapRouteCall(h.handleLoginGet))

	router.Route(string(satoriserver.ApiMessageCreate), satoriserver.WrapRouteCall(h.handleMessageCreate))
	router.Route(string(satoriserver.ApiMessageUpdate), satoriserver.WrapRouteCall(h.handleMessageUpdate))
	router.Route(string(satoriserver.ApiMessageDelete), satoriserver.WrapRouteCall(h.handleMessageDelete))
	router.Route(string(satoriserver.ApiMessageGet), satoriserver.WrapRouteCall(h.handleMessageGet))
	router.Route(string(satoriserver.ApiMessageList), satoriserver.WrapRouteCall(h.handleMessageList))

	router.Route(string(satoriserver.ApiChannelGet), satoriserver.WrapRouteCall(h.handleChannelGet))
	router.Route(string(satoriserver.ApiChannelList), satoriserver.WrapRouteCall(h.handleChannelList))
	router.Route(string(satoriserver.ApiChannelCreate), satoriserver.WrapRouteCall(h.handleChannelCreate))
	router.Route(string(satoriserver.ApiChannelUpdate), satoriserver.WrapRouteCall(h.handleChannelUpdate))
	router.Route(string(satoriserver.ApiChannelDelete), satoriserver.WrapRouteCall(h.handleChannelDelete))
	router.Route(string(satoriserver.ApiChannelMute), satoriserver.WrapRouteCall(h.handleChannelMute))

	router.Route(string(satoriserver.ApiGuildGet), satoriserver.WrapRouteCall(h.handleGuildGet))
	router.Route(string(satoriserver.ApiGuildList), satoriserver.WrapRouteCall(h.handleGuildList))
	router.Route(string(satoriserver.ApiGuildApprove), h.unsupported("guild.approve"))

	router.Route(string(satoriserver.ApiGuildMemberGet), satoriserver.WrapRouteCall(h.handleGuildMemberGet))
	router.Route(string(satoriserver.ApiGuildMemberList), satoriserver.WrapRouteCall(h.handleGuildMemberList))
	router.Route(string(satoriserver.ApiGuildMemberKick), satoriserver.WrapRouteCall(h.handleGuildMemberKick))
	router.Route(string(satoriserver.ApiGuildMemberMute), satoriserver.WrapRouteCall(h.handleGuildMemberMute))
	router.Route(string(satoriserver.ApiGuildMemberRoleSet), satoriserver.WrapRouteCall(h.handleGuildMemberRoleSet))
	router.Route(string(satoriserver.ApiGuildMemberRoleUnset), satoriserver.WrapRouteCall(h.handleGuildMemberRoleUnset))
	router.Route(string(satoriserver.ApiGuildMemberApprove), h.unsupported("guild.member.approve"))

	router.Route(string(satoriserver.ApiGuildRoleList), satoriserver.WrapRouteCall(h.handleGuildRoleList))
	router.Route(string(satoriserver.ApiGuildRoleCreate), satoriserver.WrapRouteCall(h.handleGuildRoleCreate))
	router.Route(string(satoriserver.ApiGuildRoleUpdate), satoriserver.WrapRouteCall(h.handleGuildRoleUpdate))
	router.Route(string(satoriserver.ApiGuildRoleDelete), satoriserver.WrapRouteCall(h.handleGuildRoleDelete))

	router.Route(string(satoriserver.ApiReactionCreate), satoriserver.WrapRouteCall(h.handleReactionCreate))
	router.Route(string(satoriserver.ApiReactionDelete), satoriserver.WrapRouteCall(h.handleReactionDelete))
	router.Route(string(satoriserver.ApiReactionList), satoriserver.WrapRouteCall(h.handleReactionList))
	router.Route(string(satoriserver.ApiReactionClear), satoriserver.WrapRouteCall(h.handleReactionClear))

	router.Route(string(satoriserver.ApiUserGet), satoriserver.WrapRouteCall(h.handleUserGet))
	router.Route(string(satoriserver.ApiUserChannelCreate), satoriserver.WrapRouteCall(h.handleUserChannelCreate))

	router.Route(string(satoriserver.ApiFriendList), h.unsupported("friend.list"))
	router.Route(string(satoriserver.ApiFriendApprove), h.unsupported("friend.approve"))

	router.Route("internal/*", satoriserver.WrapRouteCall(h.handleInternalRoute))
}

func (h *Handler) unsupported(action string) satoriserver.RouteCall {
	return func(request satoriserver.Request[any]) (any, error) {
		_ = request
		return nil, satoriserver.NotFound(action + " is not supported")
	}
}
