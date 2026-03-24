package model

import (
	"github.com/satori-protocol-go/satori-go/pkg/satori/model/channel"
	"github.com/satori-protocol-go/satori-go/pkg/satori/model/emoji"
	"github.com/satori-protocol-go/satori-go/pkg/satori/model/friend"
	"github.com/satori-protocol-go/satori-go/pkg/satori/model/guild"
	"github.com/satori-protocol-go/satori-go/pkg/satori/model/guildmember"
	"github.com/satori-protocol-go/satori-go/pkg/satori/model/guildrole"
	"github.com/satori-protocol-go/satori-go/pkg/satori/model/interaction"
	"github.com/satori-protocol-go/satori-go/pkg/satori/model/login"
	"github.com/satori-protocol-go/satori-go/pkg/satori/model/message"
	"github.com/satori-protocol-go/satori-go/pkg/satori/model/paginated"
	"github.com/satori-protocol-go/satori-go/pkg/satori/model/user"
)

// Pagination
type Paginated[T any] = paginated.Paginated[T]
type PaginatedSeq[T any] = paginated.PaginatedSeq[T]
type PaginatedFetcher[T any] = paginated.PaginatedFetcher[T]
type BidiPaginated[T any] = paginated.BidiPaginated[T]
type Direction = paginated.Direction
type Order = paginated.Order

// Domain models
type ChannelType = channel.ChannelType
type Channel = channel.Channel
type Emoji = emoji.Emoji
type Friend = friend.Friend
type Guild = guild.Guild
type GuildMember = guildmember.GuildMember
type GuildRole = guildrole.GuildRole
type Argv = interaction.Argv
type Button = interaction.Button
type LoginStatus = login.LoginStatus
type Login = login.Login
type LoginPartial = login.LoginPartial
type Message = message.Message
type User = user.User
