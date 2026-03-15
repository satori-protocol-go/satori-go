package qq

import (
	"context"
	"net/http"
	"time"

	botgodto "github.com/WindowsSov8forUs/botgo-plus/dto"
	botgoopenapi "github.com/WindowsSov8forUs/botgo-plus/openapi"
	botgotoken "github.com/WindowsSov8forUs/botgo-plus/token"
)

const (
	defaultWebhookPath    = "/qqbot"
	defaultAdapterName    = "qqbot"
	defaultRequestTimeout = 10 * time.Second
	defaultEventBuffer    = 128
)

var defaultQQFeatures = []string{
	"message.create",
	"message.delete",
	"upload.create",
	"login.get",
	"user.channel.create",
}

var defaultQQGuildFeatures = []string{
	"channel.get",
	"channel.list",
	"channel.create",
	"channel.update",
	"channel.delete",
	"message.create",
	"message.update",
	"message.delete",
	"message.get",
	"message.list",
	"reaction.create",
	"reaction.delete",
	"reaction.list",
	"reaction.clear",
	"upload.create",
	"guild.get",
	"guild.list",
	"guild.member.get",
	"guild.member.list",
	"guild.member.kick",
	"guild.member.mute",
	"guild.member.role.set",
	"guild.member.role.unset",
	"guild.role.list",
	"guild.role.create",
	"guild.role.update",
	"guild.role.delete",
	"login.get",
	"user.get",
	"user.channel.create",
}

// OpenAPI defines the minimum botgo-plus API set used by this adapter.
type OpenAPI interface {
	Me(ctx context.Context) (*botgodto.User, error)
	Message(ctx context.Context, channelID string, messageID string) (*botgodto.Message, error)
	PostMessage(ctx context.Context, channelID string, msg *botgodto.MessageToCreate) (*botgodto.Message, error)
	PostDirectMessage(
		ctx context.Context,
		dm *botgodto.DirectMessage,
		msg *botgodto.MessageToCreate,
	) (*botgodto.Message, error)
	RetractMessage(
		ctx context.Context,
		channelID string,
		msgID string,
		options ...botgoopenapi.RetractMessageOption,
	) error
	RetractDMMessage(
		ctx context.Context,
		guildID string,
		msgID string,
		options ...botgoopenapi.RetractMessageOption,
	) error
	CreateDirectMessage(
		ctx context.Context,
		dm *botgodto.DirectMessageToCreate,
	) (*botgodto.DirectMessage, error)
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
	RetractGroupMessage(
		ctx context.Context,
		groupID string,
		msgID string,
		options ...botgoopenapi.RetractMessageOption,
	) error
	RetractC2CMessage(
		ctx context.Context,
		userID string,
		msgID string,
		options ...botgoopenapi.RetractMessageOption,
	) error
}

type Config struct {
	AppID  uint64
	Secret string
	Token  string

	TokenURL string
	Sandbox  bool

	Path               string
	Adapter            string
	EventBuffer        int
	RequestTimeout     time.Duration
	SkipTokenInit      bool
	SkipSignatureCheck bool

	TokenInstance *botgotoken.Token
	APIV1         OpenAPI
	APIV2         OpenAPI
	HTTPClient    *http.Client

	QQFeatures      []string
	QQGuildFeatures []string
}
