package qq

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	_ "github.com/WindowsSov8forUs/botgo-plus"
	"github.com/WindowsSov8forUs/botgo-plus/openapi"
	"github.com/WindowsSov8forUs/botgo-plus/token"
	"github.com/gorilla/websocket"
	"github.com/satori-protocol-go/satori-go/pkg/satori/adapter/qq/convert"
	qqevent "github.com/satori-protocol-go/satori-go/pkg/satori/adapter/qq/event"
	"github.com/satori-protocol-go/satori-go/pkg/satori/model/event"
	"github.com/satori-protocol-go/satori-go/pkg/satori/model/login"
	"github.com/satori-protocol-go/satori-go/pkg/satori/server"
)

const (
	qqAPIBaseURL        = "https://api.sgroup.qq.com"
	qqSandboxAPIBaseURL = "https://sandbox.api.sgroup.qq.com"
)

type Adapter struct {
	server.RouterMixin

	cfg Config
	srv *server.Server

	path               string
	appID              string
	adapterName        string
	skipSignatureCheck bool
	httpClient         *http.Client
	requestTimeout     time.Duration

	apiV1 openapi.OpenAPI
	apiV2 openapi.OpenAPI
	token *token.Token

	qqFeatures      []string
	qqGuildFeatures []string

	eventCh       chan *event.Event
	publisherOnce sync.Once
	converter     *qqevent.Converter
	sender        *messageSender
	wsEnabled     bool
	wsGatewayURL  string
	wsIntents     int64
	wsShardID     uint32
	wsShardCount  uint32
	wsReconnect   time.Duration
	wsHandshake   time.Duration

	wsConnMu   sync.RWMutex
	wsWriteMu  sync.Mutex
	wsConn     *websocket.Conn
	wsSession  string
	wsSequence int64
	wsHasSeq   bool

	mu     sync.RWMutex
	logins []*login.Login
	selfID string
}

func New(cfg Config) (*Adapter, error) {
	if cfg.AppID == 0 {
		return nil, errors.New("qq adapter requires app_id")
	}
	if strings.TrimSpace(cfg.Secret) == "" {
		return nil, errors.New("qq adapter requires secret")
	}

	requestTimeout := cfg.RequestTimeout
	if requestTimeout <= 0 {
		requestTimeout = defaultRequestTimeout
	}
	buffer := cfg.EventBuffer
	if buffer <= 0 {
		buffer = defaultEventBuffer
	}

	httpClient := cfg.HTTPClient
	if httpClient == nil {
		httpClient = &http.Client{Timeout: requestTimeout}
	}
	wsIntents := cfg.WSIntents
	if wsIntents == 0 {
		wsIntents = parseWSIntentNames(cfg.WSIntentNames)
	}
	if wsIntents == 0 {
		wsIntents = defaultWSIntents
	}
	wsReconnect := cfg.WSReconnectDelay
	if wsReconnect <= 0 {
		wsReconnect = defaultWSReconnect
	}
	wsHandshake := cfg.WSHandshakeTimeout
	if wsHandshake <= 0 {
		wsHandshake = defaultWSHandshake
	}

	tokenInstance := cfg.TokenInstance
	if tokenInstance == nil {
		tokenInstance = token.BotToken(cfg.AppID, cfg.Secret, cfg.Token, token.TypeQQBot)
		if strings.TrimSpace(cfg.TokenURL) != "" {
			tokenInstance.SetTokenURL(strings.TrimSpace(cfg.TokenURL))
		}
	}
	if !cfg.SkipTokenInit {
		_ = tokenInstance.InitToken(context.Background())
	}

	apiV1 := cfg.APIV1
	apiV2 := cfg.APIV2
	if apiV1 == nil || apiV2 == nil {
		createdV1, createdV2, err := createOpenAPIClients(tokenInstance, cfg.Sandbox, requestTimeout)
		if err != nil {
			return nil, err
		}
		if apiV1 == nil {
			apiV1 = createdV1
		}
		if apiV2 == nil {
			apiV2 = createdV2
		}
	}

	adapterName := strings.TrimSpace(cfg.Adapter)
	if adapterName == "" {
		adapterName = defaultAdapterName
	}

	adapter := &Adapter{
		cfg:                cfg,
		path:               normalizeWebhookPath(cfg.Path),
		appID:              strconv.FormatUint(cfg.AppID, 10),
		adapterName:        adapterName,
		skipSignatureCheck: cfg.SkipSignatureCheck,
		httpClient:         httpClient,
		requestTimeout:     requestTimeout,
		apiV1:              apiV1,
		apiV2:              apiV2,
		token:              tokenInstance,
		qqFeatures:         valueOrDefaultFeatures(cfg.QQFeatures, defaultQQFeatures),
		qqGuildFeatures:    valueOrDefaultFeatures(cfg.QQGuildFeatures, defaultQQGuildFeatures),
		eventCh:            make(chan *event.Event, buffer),
		wsEnabled:          cfg.UseWebSocket,
		wsGatewayURL:       strings.TrimSpace(cfg.WSGatewayURL),
		wsIntents:          wsIntents,
		wsShardID:          cfg.WSShardID,
		wsShardCount:       cfg.WSShardCount,
		wsReconnect:        wsReconnect,
		wsHandshake:        wsHandshake,
	}
	adapter.converter = qqevent.New(qqevent.Dependencies{
		MessageFromDTO: convert.MessageFromDTO,
		UserFromDTO:    convert.UserFromDTO,
		MemberFromDTO:  convert.MemberFromDTO,
		GuildFromDTO:   convert.GuildFromDTO,
		ChannelFromDTO: convert.ChannelFromDTO,
		LoginForEvent: func(ctx context.Context, eventType string) *login.Login {
			return adapter.loginForEventType(ctx, eventType)
		},
		LoginForPlatform: func(ctx context.Context, platform string) *login.Login {
			return adapter.loginForPlatform(ctx, platform)
		},
	})

	adapter.sender = newMessageSender(adapter.apiV1, adapter.apiV2, convert.MessageFromDTO)
	adapter.registerRoutes()

	return adapter, nil
}

func (a *Adapter) Publisher(ctx context.Context) <-chan *event.Event {
	a.publisherOnce.Do(func() {
		go a.bootstrap(ctx)
	})
	return a.eventCh
}

func (a *Adapter) GetLogins(ctx context.Context) ([]*login.Login, error) {
	if err := a.ensureLogins(ctx); err != nil {
		return []*login.Login{}, err
	}
	a.mu.RLock()
	defer a.mu.RUnlock()

	result := make([]*login.Login, 0, len(a.logins))
	for index, item := range a.logins {
		if item == nil {
			continue
		}
		cloned := cloneLogin(item)
		cloned.Sn = int64(index)
		result = append(result, cloned)
	}
	return result, nil
}

func (a *Adapter) ProxyUrls() []string {
	return []string{}
}

func (a *Adapter) Ensure(platform string, selfID string) bool {
	if platform != "qq" && platform != "qqguild" {
		return false
	}
	if err := a.ensureLogins(context.Background()); err != nil {
		return false
	}
	a.mu.RLock()
	defer a.mu.RUnlock()
	for _, item := range a.logins {
		if item == nil || item.User == nil {
			continue
		}
		if item.Platform == platform && item.User.Id == selfID {
			return true
		}
	}
	return false
}

func (a *Adapter) HandleProxied(prefix string, rawURL string) (*server.Response, error) {
	_ = prefix
	_ = rawURL
	return nil, server.NotFound("proxy is not supported")
}

func (a *Adapter) EnsureServer(server *server.Server) {
	a.mu.Lock()
	a.srv = server
	a.mu.Unlock()
}

var _ server.Adapter = (*Adapter)(nil)
var _ server.EventPublisher = (*Adapter)(nil)
var _ server.RootRouteRegistrar = (*Adapter)(nil)
var _ server.Blockable = (*Adapter)(nil)
var _ server.Cleanable = (*Adapter)(nil)

func createOpenAPIClients(
	token *token.Token,
	sandbox bool,
	timeout time.Duration,
) (openapi.OpenAPI, openapi.OpenAPI, error) {
	v1Impl, ok := openapi.VersionMapping[openapi.APIv1]
	if !ok || v1Impl == nil {
		return nil, nil, errors.New("botgo-plus openapi v1 is not registered")
	}
	v2Impl, ok := openapi.VersionMapping[openapi.APIv2]
	if !ok || v2Impl == nil {
		return nil, nil, errors.New("botgo-plus openapi v2 is not registered")
	}

	v1 := v1Impl.Setup(token, sandbox)
	v2 := v2Impl.Setup(token, sandbox)
	if timeout > 0 {
		v1 = v1.WithTimeout(timeout)
		v2 = v2.WithTimeout(timeout)
	}
	return v1, v2, nil
}
