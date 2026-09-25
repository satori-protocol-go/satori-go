package qq

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/WindowsSov8forUs/botgo-plus/dto"
	"github.com/WindowsSov8forUs/botgo-plus/interaction/webhook"
	"github.com/WindowsSov8forUs/botgo-plus/websocket"
	"github.com/satori-protocol-go/satori-go/pkg/satori/adapter/qq/convert"
	qqevent "github.com/satori-protocol-go/satori-go/pkg/satori/adapter/qq/event"
	"github.com/satori-protocol-go/satori-go/pkg/satori/logging"
	"github.com/satori-protocol-go/satori-go/pkg/satori/model/event"
	"github.com/satori-protocol-go/satori-go/pkg/satori/model/login"
	"github.com/satori-protocol-go/satori-go/pkg/satori/server"
)

type Adapter struct {
	server.RouterMixin

	cfg Config
	srv *server.Server

	path           string
	appID          string
	adapterName    string
	httpClient     *http.Client
	requestTimeout time.Duration
	logger         logging.Logger

	appStates    map[string]*appState
	primaryAppID string

	qqFeatures      []string
	qqGuildFeatures []string

	eventContext context.Context
	cancelEvents context.CancelFunc
	eventCh      chan *event.Event
	converter    *qqevent.Converter
	wsEnabled    bool
	wsGatewayURL string
	wsIntents    int64
	wsShardID    uint32
	wsShardCount uint32
	wsReconnect  time.Duration

	wsConnMu  sync.RWMutex
	wsClients map[string]websocket.WebSocket

	auditMu sync.Mutex
	audits  map[auditKey]*auditEntry

	loginInitMu sync.Mutex
	mu          sync.RWMutex
	logins      []*login.Login
	nextLoginSN int64
	selfToApp   map[string]string
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
	logger := cfg.Logger
	if logger == nil {
		logger = logging.NewStdLogger()
	}
	wsIntents := cfg.WSIntents
	if wsIntents == 0 {
		wsIntents = parseWSIntentNames(cfg.WSIntentNames, logger)
	}
	if wsIntents == 0 {
		wsIntents = defaultWSIntents
	}
	logger.Log(context.Background(), logging.LevelInfo, fmt.Sprintf("subscribed intents=%d", wsIntents))
	wsReconnect := cfg.WSReconnectDelay
	if wsReconnect <= 0 {
		wsReconnect = defaultWSReconnect
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

	eventContext, cancelEvents := context.WithCancel(context.Background())
	adapter := &Adapter{
		cfg:             cfg,
		eventContext:    eventContext,
		cancelEvents:    cancelEvents,
		path:            normalizeWebhookPath(cfg.Path),
		appID:           primary.appID,
		adapterName:     adapterName,
		httpClient:      httpClient,
		requestTimeout:  requestTimeout,
		logger:          logger,
		appStates:       appStates,
		primaryAppID:    primaryAppID,
		qqFeatures:      valueOrDefaultFeatures(cfg.QQFeatures, defaultQQFeatures),
		qqGuildFeatures: valueOrDefaultFeatures(cfg.QQGuildFeatures, defaultQQGuildFeatures),
		eventCh:         make(chan *event.Event, buffer),
		wsEnabled:       cfg.UseWebSocket,
		wsGatewayURL:    strings.TrimSpace(cfg.WSGatewayURL),
		wsIntents:       wsIntents,
		wsShardID:       cfg.WSShardID,
		wsShardCount:    cfg.WSShardCount,
		wsReconnect:     wsReconnect,
		selfToApp:       map[string]string{},
		wsClients:       map[string]websocket.WebSocket{},
		audits:          map[auditKey]*auditEntry{},
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

	if !adapter.wsEnabled {
		for _, state := range appStates {
			handler, err := webhook.NewHandler(&state.credentials, webhook.WithEventHandler(func(ctx context.Context, payload *dto.WSPayload) error {
				return adapter.acceptPayload(withAppID(ctx, state.appID), state, payload)
			}))
			if err != nil {
				cancelEvents()
				return nil, err
			}
			state.webhook = handler
		}
	}
	adapter.registerRoutes()

	return adapter, nil
}

func (a *Adapter) Publisher(_ context.Context) <-chan *event.Event { return a.eventCh }

func (a *Adapter) Prepare(ctx context.Context) error { return a.ensureLogins(ctx) }

func (a *Adapter) GetLogins(ctx context.Context) ([]*login.Login, error) {
	if err := a.ensureLogins(ctx); err != nil {
		return []*login.Login{}, err
	}
	a.mu.RLock()
	defer a.mu.RUnlock()

	result := make([]*login.Login, 0, len(a.logins))
	for _, item := range a.logins {
		if item == nil {
			continue
		}
		cloned := cloneLogin(item)
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

func (a *Adapter) HandleProxied(ctx context.Context, prefix string, rawURL string) (*server.Response, error) {
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
var _ server.Preparable = (*Adapter)(nil)
var _ server.EventPublisher = (*Adapter)(nil)
var _ server.RootRouteRegistrar = (*Adapter)(nil)
var _ server.Blockable = (*Adapter)(nil)
var _ server.Cleanable = (*Adapter)(nil)
