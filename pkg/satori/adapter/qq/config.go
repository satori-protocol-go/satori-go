package qq

import (
	"net/http"
	"time"

	"github.com/WindowsSov8forUs/botgo-plus/dto"
	"github.com/WindowsSov8forUs/botgo-plus/media"
	"github.com/satori-protocol-go/satori-go/pkg/satori/logging"
	"golang.org/x/oauth2"
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
		dto.IntentGuildAtMessage | dto.IntentGroupMessages | dto.IntentInteraction | dto.IntentAudit,
)

var defaultQQFeatures = []string{
	"guild.get",
	"guild.member.get",
	"guild.member.list",
	"guild.member.kick",
	"guild.member.mute",
	"message.create",
	"message.delete",
	"upload.create",
	"login.get",
	"user.channel.create",
}

var defaultQQGuildFeatures = []string{
	"channel.update",
	"channel.delete",
	"message.update",
	"message.list",
	"guild.member.role.set",
	"guild.member.role.unset",
	"guild.role.list",
	"guild.role.create",
	"guild.role.update",
	"guild.role.delete",
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
	AppID       uint64
	Secret      string
	TokenURL    string
	APIBaseURL  string
	TokenSource oauth2.TokenSource
}

type Config struct {
	AppID  uint64
	Secret string

	Apps []AppConfig

	TokenURL   string
	APIBaseURL string
	Sandbox    bool

	Path             string
	Adapter          string
	EventBuffer      int
	RequestTimeout   time.Duration
	UseWebSocket     bool
	WSGatewayURL     string
	WSIntents        int64
	WSIntentNames    []string
	WSShardID        uint32
	WSShardCount     uint32
	WSReconnectDelay time.Duration

	TokenSource  oauth2.TokenSource
	HTTPClient   *http.Client // Credential-free client used by the SDK token/API transports.
	UploadConfig media.Config
	Logger       logging.Logger

	QQFeatures      []string
	QQGuildFeatures []string
}
