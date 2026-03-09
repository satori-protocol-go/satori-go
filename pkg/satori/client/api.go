package client

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
	ApiFriendApprove Api = "friend.approve"

	ApiUploadCreate Api = "upload.create"
)
