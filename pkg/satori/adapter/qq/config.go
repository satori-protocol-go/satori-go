package qq

import (
	"net/http"
	"time"

	"github.com/WindowsSov8forUs/botgo-plus/dto"
	"github.com/WindowsSov8forUs/botgo-plus/openapi"
	"github.com/WindowsSov8forUs/botgo-plus/token"
	"github.com/satori-protocol-go/satori-go/pkg/satori/logging"
)

const (
	defaultWebhookPath    = "/qqbot"
	defaultAdapterName    = "qqbot"
	defaultRequestTimeout = 10 * time.Second
	defaultEventBuffer    = 128
	defaultWSReconnect    = 5 * time.Second
)

const defaultWSIntents = int64(
	dto.IntentGuilds |
		dto.IntentGuildMembers |
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
	"message.create",
	"message.delete",
	"message.get",
	"reaction.create",
	"reaction.delete",
	"reaction.list",
	"upload.create",
	"guild.get",
	"guild.list",
	"guild.member.get",
	"guild.member.list",
	"guild.member.kick",
	"guild.member.mute",
	"login.get",
	"user.get",
	"user.channel.create",
}

type AppConfig struct {
	AppID  uint64
	Secret string
	Token  string

	TokenURL string

	TokenInstance *token.Token
	APIV1         openapi.OpenAPI
	APIV2         openapi.OpenAPI
}

type Config struct {
	AppID  uint64
	Secret string
	Token  string

	Apps []AppConfig

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

	TokenInstance *token.Token
	APIV1         openapi.OpenAPI
	APIV2         openapi.OpenAPI
	HTTPClient    *http.Client
	Logger        logging.Logger

	QQFeatures      []string
	QQGuildFeatures []string
}
