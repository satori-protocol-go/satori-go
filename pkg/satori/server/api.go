package server

type Api string

const (
	ApiChannelGet        Api = "channel.get"         // 获取群组频道
	ApiChannelList       Api = "channel.list"        // 获取群组频道列表
	ApiChannelCreate     Api = "channel.create"      // 创建群组频道
	ApiChannelUpdate     Api = "channel.update"      // 修改群组频道
	ApiChannelDelete     Api = "channel.delete"      // 删除群组频道
	ApiChannelMute       Api = "channel.mute"        // 禁言群组频道
	ApiUserChannelCreate Api = "user.channel.create" // 创建私聊频道

	ApiFriendList    Api = "friend.list"    // 获取好友列表
	ApiFriendDelete  Api = "friend.delete"  // 删除好友
	ApiFriendApprove Api = "friend.approve" // 处理好友申请

	ApiGuildGet     Api = "guild.get"     // 获取群组
	ApiGuildList    Api = "guild.list"    // 获取群组列表
	ApiGuildApprove Api = "guild.approve" // 处理群组邀请

	ApiGuildMemberGet     Api = "guild.member.get"     // 获取群组成员
	ApiGuildMemberList    Api = "guild.member.list"    // 获取群组成员列表
	ApiGuildMemberKick    Api = "guild.member.kick"    // 踢出群组成员
	ApiGuildMemberMute    Api = "guild.member.mute"    // 禁言群组成员
	ApiGuildMemberApprove Api = "guild.member.approve" // 通过群组成员申请

	ApiGuildMemberRoleSet   Api = "guild.member.role.set"   // 设置群组成员角色
	ApiGuildMemberRoleUnset Api = "guild.member.role.unset" // 取消群组成员角色
	ApiGuildRoleList        Api = "guild.role.list"         // 获取群组角色列表
	ApiGuildRoleCreate      Api = "guild.role.create"       // 创建群组角色
	ApiGuildRoleUpdate      Api = "guild.role.update"       // 修改群组角色
	ApiGuildRoleDelete      Api = "guild.role.delete"       // 删除群组角色

	ApiLoginGet Api = "login.get" // 获取登录信息

	ApiMessageCreate Api = "message.create" // 发送消息
	ApiMessageGet    Api = "message.get"    // 获取消息
	ApiMessageDelete Api = "message.delete" // 撤回消息
	ApiMessageUpdate Api = "message.update" // 编辑消息
	ApiMessageList   Api = "message.list"   // 获取消息列表

	ApiReactionCreate Api = "reaction.create" // 添加表态
	ApiReactionDelete Api = "reaction.delete" // 删除表态
	ApiReactionClear  Api = "reaction.clear"  // 清除表态
	ApiReactionList   Api = "reaction.list"   // 获取表态列表

	ApiUserGet Api = "user.get" // 获取用户信息

	ApiUploadCreate Api = "upload.create" // 文件上传
)

var apiSet = map[Api]struct{}{
	ApiChannelGet:        {},
	ApiChannelList:       {},
	ApiChannelCreate:     {},
	ApiChannelUpdate:     {},
	ApiChannelDelete:     {},
	ApiChannelMute:       {},
	ApiUserChannelCreate: {},

	ApiFriendList:    {},
	ApiFriendDelete:  {},
	ApiFriendApprove: {},

	ApiGuildGet:     {},
	ApiGuildList:    {},
	ApiGuildApprove: {},

	ApiGuildMemberGet:     {},
	ApiGuildMemberList:    {},
	ApiGuildMemberKick:    {},
	ApiGuildMemberMute:    {},
	ApiGuildMemberApprove: {},

	ApiGuildMemberRoleSet:   {},
	ApiGuildMemberRoleUnset: {},
	ApiGuildRoleList:        {},
	ApiGuildRoleCreate:      {},
	ApiGuildRoleUpdate:      {},
	ApiGuildRoleDelete:      {},

	ApiLoginGet: {},

	ApiMessageCreate: {},
	ApiMessageGet:    {},
	ApiMessageDelete: {},
	ApiMessageUpdate: {},
	ApiMessageList:   {},

	ApiReactionCreate: {},
	ApiReactionDelete: {},
	ApiReactionClear:  {},
	ApiReactionList:   {},

	ApiUserGet: {},

	ApiUploadCreate: {},
}

func isExistApi(api Api) bool {
	_, ok := apiSet[api]
	return ok
}
