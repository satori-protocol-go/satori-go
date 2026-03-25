package qq

import (
	"context"
	"errors"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/WindowsSov8forUs/botgo-plus/openapi"
	"github.com/WindowsSov8forUs/botgo-plus/token"
	"github.com/satori-protocol-go/satori-go/pkg/satori/server"
)

type appState struct {
	appID       string
	secret      string
	token       *token.Token
	apiV1       openapi.OpenAPI
	apiV2       openapi.OpenAPI
	directToken string
	selfID      string
}

type appContextKey struct{}

func withAppID(ctx context.Context, appID string) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	return context.WithValue(ctx, appContextKey{}, strings.TrimSpace(appID))
}

func appIDFromContext(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	value, _ := ctx.Value(appContextKey{}).(string)
	return strings.TrimSpace(value)
}

func buildAppStates(cfg Config, requestTimeout time.Duration) (map[string]*appState, string, error) {
	timeout := defaultRequestTimeout
	if requestTimeout > 0 {
		timeout = requestTimeout
	}

	apps := make([]AppConfig, 0, len(cfg.Apps))
	if len(cfg.Apps) > 0 {
		apps = append(apps, cfg.Apps...)
	} else {
		apps = append(apps, AppConfig{
			AppID:         cfg.AppID,
			Secret:        cfg.Secret,
			Token:         cfg.Token,
			TokenURL:      cfg.TokenURL,
			TokenInstance: cfg.TokenInstance,
			APIV1:         cfg.APIV1,
			APIV2:         cfg.APIV2,
		})
	}

	result := map[string]*appState{}
	order := make([]string, 0, len(apps))
	for _, app := range apps {
		if app.AppID == 0 {
			return nil, "", errors.New("qq adapter requires app_id")
		}
		if strings.TrimSpace(app.Secret) == "" {
			return nil, "", errors.New("qq adapter requires secret")
		}

		appID := strconv.FormatUint(app.AppID, 10)
		if _, exists := result[appID]; exists {
			return nil, "", errors.New("qq adapter app_id duplicated: " + appID)
		}

		tokenInstance := app.TokenInstance
		if tokenInstance == nil {
			tokenInstance = token.BotToken(app.AppID, app.Secret, app.Token, token.TypeQQBot)
			if strings.TrimSpace(app.TokenURL) != "" {
				tokenInstance.SetTokenURL(strings.TrimSpace(app.TokenURL))
			}
		}
		if !cfg.SkipTokenInit {
			_ = tokenInstance.InitToken(context.Background())
		}

		apiV1 := app.APIV1
		apiV2 := app.APIV2
		if apiV1 == nil || apiV2 == nil {
			createdV1, createdV2, err := createOpenAPIClients(tokenInstance, cfg.Sandbox, timeout)
			if err != nil {
				return nil, "", err
			}
			if apiV1 == nil {
				apiV1 = createdV1
			}
			if apiV2 == nil {
				apiV2 = createdV2
			}
		}

		result[appID] = &appState{
			appID:       appID,
			secret:      app.Secret,
			token:       tokenInstance,
			apiV1:       apiV1,
			apiV2:       apiV2,
			directToken: strings.TrimSpace(app.Token),
		}
		order = append(order, appID)
	}

	sort.Strings(order)
	if len(order) == 0 {
		return nil, "", errors.New("qq adapter has no app config")
	}
	return result, order[0], nil
}

func (a *Adapter) sortedAppIDs() []string {
	ids := make([]string, 0, len(a.appStates))
	for appID := range a.appStates {
		ids = append(ids, appID)
	}
	sort.Strings(ids)
	return ids
}

func (a *Adapter) primaryState() *appState {
	if state, ok := a.appStates[a.primaryAppID]; ok {
		return state
	}
	for _, appID := range a.sortedAppIDs() {
		if state, ok := a.appStates[appID]; ok {
			return state
		}
	}
	return nil
}

func (a *Adapter) stateByAppID(appID string) *appState {
	if strings.TrimSpace(appID) == "" {
		return a.primaryState()
	}
	state, ok := a.appStates[appID]
	if !ok {
		return nil
	}
	return state
}

func (a *Adapter) resolveStateBySelfID(ctx context.Context, selfID string) (*appState, error) {
	if len(a.appStates) == 0 {
		return nil, server.NotFound("qq app state not found")
	}
	if err := a.ensureLogins(ctx); err != nil {
		return nil, err
	}
	selfID = strings.TrimSpace(selfID)
	if selfID == "" {
		state := a.primaryState()
		if state == nil {
			return nil, server.NotFound("qq app state not found")
		}
		return state, nil
	}

	a.mu.RLock()
	appID := a.selfToApp[selfID]
	a.mu.RUnlock()
	if appID == "" {
		return nil, server.NotFound("login not found")
	}
	state := a.stateByAppID(appID)
	if state == nil {
		return nil, server.NotFound("qq app state not found")
	}
	return state, nil
}

func (a *Adapter) stateFromContextOrEvent(ctx context.Context, eventType string) *appState {
	if appID := appIDFromContext(ctx); appID != "" {
		if state := a.stateByAppID(appID); state != nil {
			return state
		}
	}
	platform := platformByEventType(eventType)
	if err := a.ensureLogins(ctx); err == nil {
		a.mu.RLock()
		for _, item := range a.logins {
			if item == nil || item.User == nil || item.Platform != platform {
				continue
			}
			if appID := a.selfToApp[item.User.Id]; appID != "" {
				if state := a.stateByAppID(appID); state != nil {
					a.mu.RUnlock()
					return state
				}
			}
		}
		a.mu.RUnlock()
	}
	return a.primaryState()
}
