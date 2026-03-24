package server

import (
	"net/http"
	"strings"

	"github.com/satori-protocol-go/satori-go/pkg/satori/model"
	"github.com/satori-protocol-go/satori-go/pkg/satori/protocol"
	"github.com/satori-protocol-go/satori-go/pkg/satori/types"
)

type Request[T any] struct {
	Origin   *http.Request
	Action   string
	Params   T
	Platform string
	SelfID   string
}

type RouteCall[T any, R any] func(request *Request[T]) (R, error)

type handlerChannelGet = RouteCall[ChannelParam, model.Channel]
type handlerChannelList = RouteCall[ChannelListParam, model.Paginated[model.Channel]]
type handlerChannelCreate = RouteCall[ChannelCreateParam, model.Channel]
type handlerChannelUpdate = RouteCall[ChannelUpdateParam, types.Nil]
type handlerChannelDelete = RouteCall[ChannelParam, types.Nil]
type handlerChannelMute = RouteCall[ChannelMuteParam, types.Nil]
type handlerUserChannelCreate = RouteCall[UserChannelCreateParam, model.Channel]

type handlerFriendList = RouteCall[FriendListParam, model.Paginated[model.Friend]]
type handlerFriendDelete = RouteCall[FriendDeleteParam, types.Nil]
type handlerFriendApprove = RouteCall[ApproveParam, types.Nil]

type handlerGuildGet = RouteCall[GuildGetParam, model.Guild]
type handlerGuildList = RouteCall[GuildListParam, model.Paginated[model.Guild]]
type handlerGuildApprove = RouteCall[ApproveParam, types.Nil]

type handlerGuildMemberGet = RouteCall[GuildMemberGetParam, model.GuildMember]
type handlerGuildMemberList = RouteCall[GuildListByGuildParam, model.Paginated[model.GuildMember]]
type handlerGuildMemberKick = RouteCall[GuildMemberKickParam, types.Nil]
type handlerGuildMemberMute = RouteCall[GuildMemberMuteParam, types.Nil]
type handlerGuildMemberApprove = RouteCall[ApproveParam, types.Nil]

type handlerGuildMemberRoleSet = RouteCall[GuildMemberRoleParam, types.Nil]
type handlerGuildMemberRoleUnset = RouteCall[GuildMemberRoleParam, types.Nil]
type handlerGuildRoleList = RouteCall[GuildListByGuildParam, model.Paginated[model.GuildRole]]
type handlerGuildRoleCreate = RouteCall[GuildRoleCreateParam, model.GuildRole]
type handlerGuildRoleUpdate = RouteCall[GuildRoleUpdateParam, types.Nil]
type handlerGuildRoleDelete = RouteCall[GuildRoleDeleteParam, types.Nil]

type handlerLoginGet = RouteCall[LoginGetParam, model.Login]

type handlerMessageCreate = RouteCall[MessageCreateParam, []*model.Message]
type handlerMessageGet = RouteCall[MessageOpParam, model.Message]
type handlerMessageDelete = RouteCall[MessageOpParam, types.Nil]
type handlerMessageUpdate = RouteCall[MessageUpdateParam, types.Nil]
type handlerMessageList = RouteCall[MessageListParam, model.BidiPaginated[model.Message]]

type handlerReactionCreate = RouteCall[ReactionCreateParam, types.Nil]
type handlerReactionDelete = RouteCall[ReactionDeleteParam, types.Nil]
type handlerReactionClear = RouteCall[ReactionClearParam, types.Nil]
type handlerReactionList = RouteCall[ReactionListParam, model.Paginated[model.User]]

type handlerUserGet = RouteCall[UserGetParam, model.User]

type handlerUploadCreate = RouteCall[UploadCreateParam, map[string]string]

type handlerInternal = RouteCall[InternalParam, any]

type RouterMixin struct {
	routes map[string]RouteCall[any, any]
}

func (r *RouterMixin) Route(path protocol.Api, handler RouteCall[any, any]) {
	if r.routes == nil {
		r.routes = map[string]RouteCall[any, any]{}
	}
	if handler == nil {
		return // ignore nil handler
	}
	if protocol.IsApi(path) {
		r.routes[string(path)] = handler
		return
	}
	internalAction := protocol.NormalizeInternalApi(string(path))
	if internalAction == "" {
		return
	}
	r.routes[internalAction] = handler
}

func (r *RouterMixin) RouteChannelGet(handler handlerChannelGet) {
	r.Route(protocol.ApiChannelGet, Wrapper(handler))
}

func (r *RouterMixin) RouteChannelList(handler handlerChannelList) {
	r.Route(protocol.ApiChannelList, Wrapper(handler))
}

func (r *RouterMixin) RouteChannelCreate(handler handlerChannelCreate) {
	r.Route(protocol.ApiChannelCreate, Wrapper(handler))
}

func (r *RouterMixin) RouteChannelUpdate(handler handlerChannelUpdate) {
	r.Route(protocol.ApiChannelUpdate, Wrapper(handler))
}

func (r *RouterMixin) RouteChannelDelete(handler handlerChannelDelete) {
	r.Route(protocol.ApiChannelDelete, Wrapper(handler))
}

func (r *RouterMixin) RouteChannelMute(handler handlerChannelMute) {
	r.Route(protocol.ApiChannelMute, Wrapper(handler))
}

func (r *RouterMixin) RouteUserChannelCreate(handler handlerUserChannelCreate) {
	r.Route(protocol.ApiUserChannelCreate, Wrapper(handler))
}

func (r *RouterMixin) RouteFriendList(handler handlerFriendList) {
	r.Route(protocol.ApiFriendList, Wrapper(handler))
}

func (r *RouterMixin) RouteFriendDelete(handler handlerFriendDelete) {
	r.Route(protocol.ApiFriendDelete, Wrapper(handler))
}

func (r *RouterMixin) RouteFriendApprove(handler handlerFriendApprove) {
	r.Route(protocol.ApiFriendApprove, Wrapper(handler))
}

func (r *RouterMixin) RouteGuildGet(handler handlerGuildGet) {
	r.Route(protocol.ApiGuildGet, Wrapper(handler))
}

func (r *RouterMixin) RouteGuildList(handler handlerGuildList) {
	r.Route(protocol.ApiGuildList, Wrapper(handler))
}

func (r *RouterMixin) RouteGuildApprove(handler handlerGuildApprove) {
	r.Route(protocol.ApiGuildApprove, Wrapper(handler))
}

func (r *RouterMixin) RouteGuildMemberGet(handler handlerGuildMemberGet) {
	r.Route(protocol.ApiGuildMemberGet, Wrapper(handler))
}

func (r *RouterMixin) RouteGuildMemberList(handler handlerGuildMemberList) {
	r.Route(protocol.ApiGuildMemberList, Wrapper(handler))
}

func (r *RouterMixin) RouteGuildMemberKick(handler handlerGuildMemberKick) {
	r.Route(protocol.ApiGuildMemberKick, Wrapper(handler))
}

func (r *RouterMixin) RouteGuildMemberMute(handler handlerGuildMemberMute) {
	r.Route(protocol.ApiGuildMemberMute, Wrapper(handler))
}

func (r *RouterMixin) RouteGuildMemberApprove(handler handlerGuildMemberApprove) {
	r.Route(protocol.ApiGuildMemberApprove, Wrapper(handler))
}

func (r *RouterMixin) RouteGuildMemberRoleSet(handler handlerGuildMemberRoleSet) {
	r.Route(protocol.ApiGuildMemberRoleSet, Wrapper(handler))
}

func (r *RouterMixin) RouteGuildMemberRoleUnset(handler handlerGuildMemberRoleUnset) {
	r.Route(protocol.ApiGuildMemberRoleUnset, Wrapper(handler))
}

func (r *RouterMixin) RouteGuildRoleList(handler handlerGuildRoleList) {
	r.Route(protocol.ApiGuildRoleList, Wrapper(handler))
}

func (r *RouterMixin) RouteGuildRoleCreate(handler handlerGuildRoleCreate) {
	r.Route(protocol.ApiGuildRoleCreate, Wrapper(handler))
}

func (r *RouterMixin) RouteGuildRoleUpdate(handler handlerGuildRoleUpdate) {
	r.Route(protocol.ApiGuildRoleUpdate, Wrapper(handler))
}

func (r *RouterMixin) RouteGuildRoleDelete(handler handlerGuildRoleDelete) {
	r.Route(protocol.ApiGuildRoleDelete, Wrapper(handler))
}

func (r *RouterMixin) RouteLoginGet(handler handlerLoginGet) {
	r.Route(protocol.ApiLoginGet, Wrapper(handler))
}

func (r *RouterMixin) RouteMessageCreate(handler handlerMessageCreate) {
	r.Route(protocol.ApiMessageCreate, Wrapper(handler))
}

func (r *RouterMixin) RouteMessageGet(handler handlerMessageGet) {
	r.Route(protocol.ApiMessageGet, Wrapper(handler))
}

func (r *RouterMixin) RouteMessageDelete(handler handlerMessageDelete) {
	r.Route(protocol.ApiMessageDelete, Wrapper(handler))
}

func (r *RouterMixin) RouteMessageUpdate(handler handlerMessageUpdate) {
	r.Route(protocol.ApiMessageUpdate, Wrapper(handler))
}

func (r *RouterMixin) RouteMessageList(handler handlerMessageList) {
	r.Route(protocol.ApiMessageList, Wrapper(handler))
}

func (r *RouterMixin) RouteReactionCreate(handler handlerReactionCreate) {
	r.Route(protocol.ApiReactionCreate, Wrapper(handler))
}

func (r *RouterMixin) RouteReactionDelete(handler handlerReactionDelete) {
	r.Route(protocol.ApiReactionDelete, Wrapper(handler))
}

func (r *RouterMixin) RouteReactionClear(handler handlerReactionClear) {
	r.Route(protocol.ApiReactionClear, Wrapper(handler))
}

func (r *RouterMixin) RouteReactionList(handler handlerReactionList) {
	r.Route(protocol.ApiReactionList, Wrapper(handler))
}

func (r *RouterMixin) RouteUserGet(handler handlerUserGet) {
	r.Route(protocol.ApiUserGet, Wrapper(handler))
}

func (r *RouterMixin) RouteUploadCreate(handler handlerUploadCreate) {
	r.Route(protocol.ApiUploadCreate, Wrapper(handler))
}

func (r *RouterMixin) RouteInternal(path string, handler handlerInternal) {
	r.Route(protocol.ParseApi(path), Wrapper(handler))
}

func (r *RouterMixin) Routes() map[string]RouteCall[any, any] {
	if r.routes == nil {
		r.routes = map[string]RouteCall[any, any]{}
	}
	return r.routes
}

func matchRoute(routes map[string]RouteCall[any, any], action string) (RouteCall[any, any], bool) {
	if len(routes) == 0 {
		return nil, false
	}
	if handler, ok := routes[action]; ok {
		return handler, true
	}
	if strings.HasPrefix(action, "internal") {
		handler, ok := routes["internal/*"]
		return handler, ok
	}
	return nil, false
}

func bindParams[T any](request *Request[any]) (*Request[T], error) {
	params, err := decodeParams[T](request.Params)
	if err != nil {
		return &Request[T]{}, err
	}
	return &Request[T]{
		Origin:   request.Origin,
		Action:   request.Action,
		Params:   params,
		Platform: request.Platform,
		SelfID:   request.SelfID,
	}, nil
}

func Wrapper[T any, R any](handler RouteCall[T, R]) RouteCall[any, any] {
	return func(request *Request[any]) (any, error) {
		typedRequest, err := bindParams[T](request)
		if err != nil {
			return nil, err
		}
		result, err := handler(typedRequest)
		if err != nil {
			return nil, err
		}
		return any(result), nil
	}
}
