package event

import (
	"context"

	"github.com/WindowsSov8forUs/botgo-plus/dto"
	"github.com/satori-protocol-go/satori-go/pkg/satori/model/channel"
	"github.com/satori-protocol-go/satori-go/pkg/satori/model/guild"
	"github.com/satori-protocol-go/satori-go/pkg/satori/model/guildmember"
	"github.com/satori-protocol-go/satori-go/pkg/satori/model/login"
	"github.com/satori-protocol-go/satori-go/pkg/satori/model/message"
	"github.com/satori-protocol-go/satori-go/pkg/satori/model/user"
)

const (
	eventTypeChannelAdded   = "channel-added"
	eventTypeChannelUpdated = "channel-updated"
	eventTypeChannelRemoved = "channel-removed"
	eventTypeFriendAdded    = "friend-added"
	eventTypeFriendRemoved  = "friend-removed"
)

type Dependencies struct {
	MessageFromDTO   func(input *dto.Message, platform string) *message.Message
	UserFromDTO      func(input *dto.User) *user.User
	MemberFromDTO    func(input *dto.Member) *guildmember.GuildMember
	GuildFromDTO     func(input *dto.Guild) *guild.Guild
	ChannelFromDTO   func(input *dto.Channel) *channel.Channel
	LoginForEvent    func(ctx context.Context, eventType string) *login.Login
	LoginForPlatform func(ctx context.Context, platform string) *login.Login
}

type Converter struct {
	deps Dependencies
}

func New(deps Dependencies) *Converter {
	return &Converter{deps: deps}
}

func (c *Converter) messageFromDTO(input *dto.Message, platform string) *message.Message {
	if c.deps.MessageFromDTO == nil {
		return &message.Message{}
	}
	result := c.deps.MessageFromDTO(input, platform)
	if result == nil {
		return &message.Message{}
	}
	return result
}

func (c *Converter) userFromDTO(input *dto.User) *user.User {
	if c.deps.UserFromDTO == nil {
		return nil
	}
	return c.deps.UserFromDTO(input)
}

func (c *Converter) memberFromDTO(input *dto.Member) *guildmember.GuildMember {
	if c.deps.MemberFromDTO == nil {
		return nil
	}
	return c.deps.MemberFromDTO(input)
}

func (c *Converter) guildFromDTO(input *dto.Guild) *guild.Guild {
	if c.deps.GuildFromDTO == nil {
		return nil
	}
	return c.deps.GuildFromDTO(input)
}

func (c *Converter) channelFromDTO(input *dto.Channel) *channel.Channel {
	if c.deps.ChannelFromDTO == nil {
		return nil
	}
	return c.deps.ChannelFromDTO(input)
}

func (c *Converter) loginForEvent(ctx context.Context, eventType string) *login.Login {
	if c.deps.LoginForEvent == nil {
		return nil
	}
	return c.deps.LoginForEvent(ctx, eventType)
}

func (c *Converter) loginForPlatform(ctx context.Context, platform string) *login.Login {
	if c.deps.LoginForPlatform == nil {
		return nil
	}
	return c.deps.LoginForPlatform(ctx, platform)
}
