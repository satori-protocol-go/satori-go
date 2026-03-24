package protocol

import "strings"

type Api string

const (
	ApiMessageCreate Api = "message.create"
	ApiMessageUpdate Api = "message.update"
	ApiMessageGet    Api = "message.get"
	ApiMessageDelete Api = "message.delete"
	ApiMessageList   Api = "message.list"

	ApiChannelGet        Api = "channel.get"
	ApiChannelList       Api = "channel.list"
	ApiChannelCreate     Api = "channel.create"
	ApiChannelUpdate     Api = "channel.update"
	ApiChannelDelete     Api = "channel.delete"
	ApiChannelMute       Api = "channel.mute"
	ApiUserChannelCreate Api = "user.channel.create"

	ApiGuildGet     Api = "guild.get"
	ApiGuildList    Api = "guild.list"
	ApiGuildApprove Api = "guild.approve"

	ApiGuildMemberList      Api = "guild.member.list"
	ApiGuildMemberGet       Api = "guild.member.get"
	ApiGuildMemberKick      Api = "guild.member.kick"
	ApiGuildMemberMute      Api = "guild.member.mute"
	ApiGuildMemberApprove   Api = "guild.member.approve"
	ApiGuildMemberRoleSet   Api = "guild.member.role.set"
	ApiGuildMemberRoleUnset Api = "guild.member.role.unset"

	ApiGuildRoleList   Api = "guild.role.list"
	ApiGuildRoleCreate Api = "guild.role.create"
	ApiGuildRoleUpdate Api = "guild.role.update"
	ApiGuildRoleDelete Api = "guild.role.delete"

	ApiReactionCreate Api = "reaction.create"
	ApiReactionDelete Api = "reaction.delete"
	ApiReactionClear  Api = "reaction.clear"
	ApiReactionList   Api = "reaction.list"

	ApiLoginGet Api = "login.get"

	ApiUserGet       Api = "user.get"
	ApiFriendList    Api = "friend.list"
	ApiFriendDelete  Api = "friend.delete"
	ApiFriendApprove Api = "friend.approve"

	ApiUploadCreate Api = "upload.create"
)

const (
	ApiMetaGet           Api = "meta"
	ApiMetaWebhookCreate Api = "meta/webhook.create"
	ApiMetaWebhookDelete Api = "meta/webhook.delete"
	ApiAdminLoginList    Api = "admin/login.list"
)

var apiSet = map[Api]struct{}{
	ApiMessageCreate: {},
	ApiMessageUpdate: {},
	ApiMessageGet:    {},
	ApiMessageDelete: {},
	ApiMessageList:   {},

	ApiChannelGet:        {},
	ApiChannelList:       {},
	ApiChannelCreate:     {},
	ApiChannelUpdate:     {},
	ApiChannelDelete:     {},
	ApiChannelMute:       {},
	ApiUserChannelCreate: {},

	ApiGuildGet:     {},
	ApiGuildList:    {},
	ApiGuildApprove: {},

	ApiGuildMemberList:      {},
	ApiGuildMemberGet:       {},
	ApiGuildMemberKick:      {},
	ApiGuildMemberMute:      {},
	ApiGuildMemberApprove:   {},
	ApiGuildMemberRoleSet:   {},
	ApiGuildMemberRoleUnset: {},

	ApiGuildRoleList:   {},
	ApiGuildRoleCreate: {},
	ApiGuildRoleUpdate: {},
	ApiGuildRoleDelete: {},

	ApiReactionCreate: {},
	ApiReactionDelete: {},
	ApiReactionClear:  {},
	ApiReactionList:   {},

	ApiLoginGet: {},

	ApiUserGet:       {},
	ApiFriendList:    {},
	ApiFriendDelete:  {},
	ApiFriendApprove: {},

	ApiUploadCreate: {},
}

func (a Api) String() string {
	return string(a)
}

func ParseApi(raw string) Api {
	return Api(strings.TrimSpace(raw))
}

func IsApi(api Api) bool {
	_, ok := apiSet[api]
	return ok
}
