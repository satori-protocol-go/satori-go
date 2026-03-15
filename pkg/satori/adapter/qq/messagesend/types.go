package messagesend

import (
	"context"

	botgodto "github.com/WindowsSov8forUs/botgo-plus/dto"
	"github.com/satori-protocol-go/satori-go/pkg/satori/model/message"
)

type GuildAPI interface {
	PostMessage(ctx context.Context, channelID string, msg *botgodto.MessageToCreate) (*botgodto.Message, error)
	PostDirectMessage(
		ctx context.Context,
		dm *botgodto.DirectMessage,
		msg *botgodto.MessageToCreate,
	) (*botgodto.Message, error)
}

type GuildMultipartAPI interface {
	PostMessageMultipart(
		ctx context.Context,
		channelID string,
		msg *botgodto.MessageToCreate,
		fileImageData []byte,
	) (*botgodto.Message, error)
	PostDirectMessageMultipart(
		ctx context.Context,
		dm *botgodto.DirectMessage,
		msg *botgodto.MessageToCreate,
		fileImageData []byte,
	) (*botgodto.Message, error)
}

type QQAPI interface {
	PostGroupMessage(
		ctx context.Context,
		groupID string,
		msg botgodto.APIMessage,
	) (*botgodto.GroupMessageResponse, error)
	PostC2CMessage(
		ctx context.Context,
		userID string,
		msg botgodto.APIMessage,
	) (*botgodto.C2CMessageResponse, error)
}

type MessageConverter func(input *botgodto.Message, platform string) *message.Message

type Dependencies struct {
	GuildAPI          GuildAPI
	GuildMultipartAPI GuildMultipartAPI
	QQAPI             QQAPI
	ConvertMessage    MessageConverter
}

type Sender struct {
	guildAPI          GuildAPI
	guildMultipartAPI GuildMultipartAPI
	qqAPI             QQAPI
	convertMessage    MessageConverter
}

type Referrer struct {
	Direct    bool
	MsgID     string
	MsgSeq    int
	HasMsgSeq bool
}

type CreateInput struct {
	Platform  string
	ChannelID string
	Content   string
	Referrer  Referrer
}

func New(deps Dependencies) *Sender {
	return &Sender{
		guildAPI:          deps.GuildAPI,
		guildMultipartAPI: deps.GuildMultipartAPI,
		qqAPI:             deps.QQAPI,
		convertMessage:    deps.ConvertMessage,
	}
}
