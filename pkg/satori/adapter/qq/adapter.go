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
	botgotoken "github.com/WindowsSov8forUs/botgo-plus/token"
	qqaction "github.com/satori-protocol-go/satori-go/pkg/satori/adapter/qq/action"
	qqcodec "github.com/satori-protocol-go/satori-go/pkg/satori/adapter/qq/codec"
	qqeventconv "github.com/satori-protocol-go/satori-go/pkg/satori/adapter/qq/eventconv"
	"github.com/satori-protocol-go/satori-go/pkg/satori/model/event"
	"github.com/satori-protocol-go/satori-go/pkg/satori/model/login"
	satoriserver "github.com/satori-protocol-go/satori-go/pkg/satori/server"
)

const (
	qqAPIBaseURL        = "https://api.sgroup.qq.com"
	qqSandboxAPIBaseURL = "https://sandbox.api.sgroup.qq.com"
)

type Adapter struct {
	satoriserver.RouterMixin

	cfg Config

	path               string
	appID              string
	adapterName        string
	skipSignatureCheck bool
	httpClient         *http.Client
	requestTimeout     time.Duration

	apiV1 OpenAPI
	apiV2 OpenAPI
	token *botgotoken.Token

	qqFeatures      []string
	qqGuildFeatures []string

	eventCh       chan *event.Event
	publisherOnce sync.Once
	converter     *qqeventconv.Converter
	actions       *qqaction.Handler

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

	tokenInstance := cfg.TokenInstance
	if tokenInstance == nil {
		tokenInstance = botgotoken.BotToken(cfg.AppID, cfg.Secret, cfg.Token, botgotoken.TypeQQBot)
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
	}
	adapter.converter = qqeventconv.New(qqeventconv.Dependencies{
		MessageFromDTO: qqcodec.MessageFromDTO,
		UserFromDTO:    qqcodec.UserFromDTO,
		MemberFromDTO:  qqcodec.MemberFromDTO,
		GuildFromDTO:   qqcodec.GuildFromDTO,
		ChannelFromDTO: qqcodec.ChannelFromDTO,
		LoginForEvent: func(ctx context.Context, eventType string) *login.Login {
			return adapter.loginForEventType(ctx, eventType)
		},
		LoginForPlatform: func(ctx context.Context, platform string) *login.Login {
			return adapter.loginForPlatform(ctx, platform)
		},
	})
	adapter.actions = qqaction.New(qqaction.Dependencies{
		APIV1:        adapter.apiV1,
		APIV2:        adapter.apiV2,
		EnsureLogins: adapter.ensureLogins,
		FindLogin:    adapter.findLogin,
		HandleInternal: func(
			request satoriserver.Request[map[string]any],
			path string,
		) (*satoriserver.Response, error) {
			return adapter.HandleInternal(request, path)
		},
	})
	adapter.actions.Register(&adapter.RouterMixin)
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

func (a *Adapter) HandleProxied(prefix string, rawURL string) (*satoriserver.Response, error) {
	_ = prefix
	_ = rawURL
	return nil, nil
}

var _ satoriserver.Adapter = (*Adapter)(nil)
var _ satoriserver.EventPublisher = (*Adapter)(nil)
var _ satoriserver.RootRouteProvider = (*Adapter)(nil)
