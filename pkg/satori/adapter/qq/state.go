package qq

import (
	"context"
	"errors"
	"net/http"
	"sort"
	"strconv"
	"time"

	botgo "github.com/WindowsSov8forUs/botgo-plus"
	"github.com/WindowsSov8forUs/botgo-plus/constant"
	"github.com/WindowsSov8forUs/botgo-plus/media"
	native "github.com/WindowsSov8forUs/botgo-plus/openapi/v1"
	"github.com/WindowsSov8forUs/botgo-plus/token"
	"github.com/satori-protocol-go/satori-go/pkg/satori/server"
	"golang.org/x/oauth2"
)

type appState struct {
	appID          string
	credentials    token.QQBotCredentials
	token          oauth2.TokenSource
	api            *native.Client
	uploader       *media.Uploader
	webhook        http.Handler
	selfID         string
	readyShards    map[uint32]bool // Protected by Adapter.mu.
	expectedShards int
}

type appContextKey struct{}
type nativeHeadersKey struct{}

func withAppID(ctx context.Context, appID string) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	return context.WithValue(ctx, appContextKey{}, appID)
}
func appIDFromContext(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	value, _ := ctx.Value(appContextKey{}).(string)
	return value
}

// Native request headers are applied at the SDK's configurable HTTP boundary.
// Authentication, retries and response classification remain owned by the SDK.
type nativeTransport struct{ http.RoundTripper }

func (t nativeTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	if headers, ok := request.Context().Value(nativeHeadersKey{}).(http.Header); ok {
		request = request.Clone(request.Context())
		for _, key := range []string{"Content-Type", "Accept", "Range", "If-None-Match", "If-Modified-Since"} {
			if values, exists := headers[key]; exists {
				request.Header[key] = append([]string(nil), values...)
			}
		}
	}
	return t.RoundTripper.RoundTrip(request)
}

func buildAppStates(cfg Config, timeout time.Duration) (map[string]*appState, string, error) {
	apps := append([]AppConfig(nil), cfg.Apps...)
	if len(apps) == 0 {
		apps = []AppConfig{{AppID: cfg.AppID, Secret: cfg.Secret, TokenURL: cfg.TokenURL, APIBaseURL: cfg.APIBaseURL, TokenSource: cfg.TokenSource}}
	}
	states := map[string]*appState{}
	for _, app := range apps {
		if app.AppID == 0 || app.Secret == "" {
			return nil, "", errors.New("QQ AppID and Secret are required")
		}
		id := strconv.FormatUint(app.AppID, 10)
		if states[id] != nil {
			return nil, "", errors.New("duplicate QQ AppID: " + id)
		}
		credentials := token.QQBotCredentials{AppID: id, AppSecret: app.Secret}
		source := app.TokenSource
		if source == nil {
			options := []token.Option{token.WithRequestTimeout(timeout)}
			if cfg.HTTPClient != nil {
				options = append(options, token.WithHTTPClient(cfg.HTTPClient))
			}
			endpoint := app.TokenURL
			if endpoint == "" {
				endpoint = cfg.TokenURL
			}
			if endpoint != "" {
				options = append(options, token.WithEndpoint(endpoint))
			}
			source = token.NewQQBotTokenSource(&credentials, options...)
		}
		base := app.APIBaseURL
		if base == "" {
			base = cfg.APIBaseURL
		}
		if base == "" {
			base = constant.APIDomain
			if cfg.Sandbox {
				base = constant.SandBoxAPIDomain
			}
		}
		transport := http.DefaultTransport
		if cfg.HTTPClient != nil && cfg.HTTPClient.Transport != nil {
			transport = cfg.HTTPClient.Transport
		}
		api, err := botgo.NewClient(id, source, native.WithBaseURL(base), native.WithRequestTimeout(timeout), native.WithHTTPTransport(nativeTransport{transport}))
		if err != nil {
			return nil, "", err
		}
		uploadConfig := cfg.UploadConfig
		if uploadConfig.HTTPClient == nil {
			uploadConfig.HTTPClient = cfg.HTTPClient
		}
		uploader, err := media.NewUploader(api, uploadConfig)
		if err != nil {
			return nil, "", err
		}
		states[id] = &appState{appID: id, credentials: credentials, token: source, api: api, uploader: uploader}
	}
	ids := make([]string, 0, len(states))
	for id := range states {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return states, ids[0], nil
}

func (a *Adapter) sortedAppIDs() []string {
	ids := make([]string, 0, len(a.appStates))
	for id := range a.appStates {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids
}
func (a *Adapter) primaryState() *appState             { return a.appStates[a.primaryAppID] }
func (a *Adapter) stateByAppID(appID string) *appState { return a.appStates[appID] }

func (a *Adapter) resolveStateBySelfID(ctx context.Context, selfID string) (*appState, error) {
	if err := a.ensureLogins(ctx); err != nil {
		return nil, err
	}
	a.mu.RLock()
	id := a.selfToApp[selfID]
	a.mu.RUnlock()
	if id == "" {
		return nil, server.NotFound("QQ login not found")
	}
	return a.appStates[id], nil
}

func (a *Adapter) stateFromContextOrEvent(ctx context.Context, _ string) *appState {
	if id := appIDFromContext(ctx); id != "" {
		return a.appStates[id]
	}
	if len(a.appStates) == 1 {
		return a.primaryState()
	}
	return nil
}
