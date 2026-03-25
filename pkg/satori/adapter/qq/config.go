package qq

import (
	"net/http"
	"time"

	"github.com/WindowsSov8forUs/botgo-plus/dto"
	"github.com/WindowsSov8forUs/botgo-plus/openapi"
	"github.com/WindowsSov8forUs/botgo-plus/token"
)

const (
	defaultWebhookPath    = "/qqbot"
	defaultAdapterName    = "qqbot"
	defaultRequestTimeout = 10 * time.Second
	defaultEventBuffer    = 128
	defaultWSReconnect    = 5 * time.Second
	defaultWSHandshake    = 30 * time.Second
)

const defaultWSIntents = int64(
	dto.IntentGuilds |
		dto.IntentGuildMembers |
		dto.IntentGuildMessageReactions |
		dto.IntentGroupAndC2CEvent |
		dto.IntentInteraction |
		dto.IntentMessageAudit |
		dto.IntentPublicGuildMessages,
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
	UseWebSocket       bool
	WSGatewayURL       string
	WSIntents          int64
	WSIntentNames      []string
	WSShardID          uint32
	WSShardCount       uint32
	WSReconnectDelay   time.Duration
	WSHandshakeTimeout time.Duration

	TokenInstance *token.Token
	APIV1         openapi.OpenAPI
	APIV2         openapi.OpenAPI
	HTTPClient    *http.Client

	QQFeatures      []string
	QQGuildFeatures []string
}
