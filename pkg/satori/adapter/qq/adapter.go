package qq

import (
	"context"
	"errors"
	"net/http"
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

	appStates    map[string]*appState
	primaryAppID string

	apiV1 openapi.OpenAPI
	apiV2 openapi.OpenAPI
	token *token.Token

	qqFeatures      []string
	qqGuildFeatures []string

	eventCh       chan *event.Event
	publisherOnce sync.Once
	converter     *qqevent.Converter
	wsEnabled     bool
	wsGatewayURL  string
	wsIntents     int64
	wsShardID     uint32
	wsShardCount  uint32
	wsReconnect   time.Duration
	wsHandshake   time.Duration

	wsConnMu  sync.RWMutex
	wsWriteMu sync.Mutex
	wsConn    *websocket.Conn
	wsConns   map[string]*websocket.Conn

	auditMu      sync.Mutex
	auditWaiters map[string][]chan string

	mu        sync.RWMutex
	logins    []*login.Login
	selfID    string
	selfToApp map[string]string
}

func New(cfg Config) (*Adapter, error) {
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

	adapterName := strings.TrimSpace(cfg.Adapter)
	if adapterName == "" {
		adapterName = defaultAdapterName
	}

	appStates, primaryAppID, err := buildAppStates(cfg, requestTimeout)
	if err != nil {
		return nil, err
	}
	primary := appStates[primaryAppID]
	if primary == nil {
		return nil, errors.New("qq adapter primary app state not found")
	}

	adapter := &Adapter{
		cfg:                cfg,
		path:               normalizeWebhookPath(cfg.Path),
		appID:              primary.appID,
		adapterName:        adapterName,
		skipSignatureCheck: cfg.SkipSignatureCheck,
		httpClient:         httpClient,
		requestTimeout:     requestTimeout,
		appStates:          appStates,
		primaryAppID:       primaryAppID,
		apiV1:              primary.apiV1,
		apiV2:              primary.apiV2,
		token:              primary.token,
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
		selfToApp:          map[string]string{},
		wsConns:            map[string]*websocket.Conn{},
		auditWaiters:       map[string][]chan string{},
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
