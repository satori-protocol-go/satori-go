package model

import (
	"github.com/satori-protocol-go/satori-go/pkg/satori/model/channel"
	"github.com/satori-protocol-go/satori-go/pkg/satori/model/define"
	"github.com/satori-protocol-go/satori-go/pkg/satori/model/emoji"
	"github.com/satori-protocol-go/satori-go/pkg/satori/model/friend"
	"github.com/satori-protocol-go/satori-go/pkg/satori/model/guild"
	"github.com/satori-protocol-go/satori-go/pkg/satori/model/guildmember"
	"github.com/satori-protocol-go/satori-go/pkg/satori/model/guildrole"
	"github.com/satori-protocol-go/satori-go/pkg/satori/model/interaction"
	"github.com/satori-protocol-go/satori-go/pkg/satori/model/login"
	"github.com/satori-protocol-go/satori-go/pkg/satori/model/message"
	"github.com/satori-protocol-go/satori-go/pkg/satori/model/user"
)

// 分页列表
type Paginated[T any] = define.Paginated[T]

// 双向分页列表
type BidiPaginated[T any] = define.BidiPaginated[T]

type Direction = define.Direction
type Order = define.Order

// 频道类型
type ChannelType = channel.ChannelType

// 频道
type Channel = channel.Channel

// 表情
type Emoji = emoji.Emoji

// 好友
type Friend = friend.Friend

// 群组
type Guild = guild.Guild

// 群组成员
type GuildMember = guildmember.GuildMember

// 群组角色
type GuildRole = guildrole.GuildRole

// 交互指令
type Argv = interaction.Argv

// 交互按钮
type Button = interaction.Button

type LoginStatus = login.LoginStatus

// 登录信息
type Login = login.Login

// 消息
type Message = message.Message

// 用户
type User = user.User
