package satori

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"mime/multipart"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/satori-protocol-go/satori-go/pkg/satori/client"
	"github.com/satori-protocol-go/satori-go/pkg/satori/model/event"
	"github.com/satori-protocol-go/satori-go/pkg/satori/model/login"
	satoriserver "github.com/satori-protocol-go/satori-go/pkg/satori/server"
)

const defaultEventBuffer = 128

type Config struct {
	Host       string
	Port       int
	Path       string
	Version    string
	Token      string
	PostUpload bool

	EventBuffer int
	HTTPClient  *http.Client
}

type Adapter struct {
	satoriserver.RouterMixin

	app        *client.App
	postUpload bool
	httpClient *http.Client

	eventCh chan *event.Event
	runOnce sync.Once
}

func New(cfg Config) (*Adapter, error) {
	app, err := client.NewApp(client.WebSocketConfig{
		Host:    cfg.Host,
		Port:    cfg.Port,
		Path:    cfg.Path,
		Version: cfg.Version,
		Token:   cfg.Token,
	})
	if err != nil {
		return nil, err
	}

	buffer := cfg.EventBuffer
	if buffer <= 0 {
		buffer = defaultEventBuffer
	}
	adapter := &Adapter{
		app:        app,
		postUpload: cfg.PostUpload,
		httpClient: cfg.HTTPClient,
		eventCh:    make(chan *event.Event, buffer),
	}
	if adapter.httpClient == nil {
		adapter.httpClient = &http.Client{Timeout: 300 * time.Second}
	}

	adapter.Route("internal/*", adapter.handleRoute)
	for _, action := range supportedAPIActions {
		if !cfg.PostUpload && action == string(client.ApiUploadCreate) {
			continue
		}
		adapter.Route(action, adapter.handleRoute)
	}

	adapter.app.Register(func(account *client.Account, evt *event.Event) error {
		_ = account
		if evt == nil {
			return nil
		}
		select {
		case adapter.eventCh <- evt:
		default:
			// Drop events when consumer is slower than producer to avoid blocking client network loops.
		}
		return nil
	})

	return adapter, nil
}

func (a *Adapter) EnsureServer(server *satoriserver.Server) {
	_ = server
}

func (a *Adapter) Publisher(ctx context.Context) <-chan *event.Event {
	a.runOnce.Do(func() {
		go func() {
			if err := a.app.Run(ctx); err != nil && !errors.Is(err, context.Canceled) {
				log.Printf("[satori-adapter] client app stopped: %v", err)
			}
		}()
	})
	return a.eventCh
}

func (a *Adapter) GetLogins(ctx context.Context) ([]*login.Login, error) {
	_ = ctx
	account := a.Account()
	if account == nil || account.SelfInfo == nil {
		return []*login.Login{}, nil
	}
	return []*login.Login{account.SelfInfo}, nil
}

func (a *Adapter) ProxyUrls() []string {
	account := a.Account()
	if account == nil {
		return []string{}
	}
	return account.ProxyURLs()
}

func (a *Adapter) Ensure(platform string, selfID string) bool {
	account := a.Account()
	if account == nil {
		return false
	}
	return account.Platform() == platform && account.SelfID() == selfID
}

func (a *Adapter) HandleInternal(request satoriserver.Request[map[string]any], path string) (*satoriserver.Response, error) {
	path = strings.TrimSpace(path)
	if strings.HasPrefix(path, "_api") {
		account := a.Account()
		if account == nil {
			return nil, satoriserver.NotFound("no account found")
		}

		action := strings.TrimPrefix(path, "_api")
		action = strings.TrimPrefix(action, "/")

		params := request.Params
		if params == nil {
			params = map[string]any{}
		}

		method := http.MethodGet
		ctx := context.Background()
		if request.Origin != nil {
			method = request.Origin.Method
			ctx = request.Origin.Context()
		}

		raw, err := account.Protocol.CallAPI(ctx, action, params, false, method)
		if err != nil {
			return nil, err
		}
		response := satoriserver.NewResponse(http.StatusOK, raw)
		response.Header.Set("Content-Type", "application/json")
		return response, nil
	}

	if account := a.Account(); account != nil {
		internalURL := fmt.Sprintf(
			"internal:%s/%s/%s",
			account.Platform(),
			account.SelfID(),
			strings.TrimPrefix(path, "/"),
		)
		data, err := account.Protocol.Download(requestContext(request.Origin), internalURL)
		if err != nil {
			return nil, err
		}
		return satoriserver.NewResponse(http.StatusOK, data), nil
	}

	if path == "" {
		return nil, satoriserver.NotFound("path is empty")
	}
	return a.fetchURL(requestContext(request.Origin), path)
}

func (a *Adapter) HandleProxied(prefix string, rawURL string) (*satoriserver.Response, error) {
	_ = prefix
	return a.fetchURL(context.Background(), rawURL)
}

func (a *Adapter) Account() *client.Account {
	accounts := a.app.Accounts()
	for _, account := range accounts {
		return account
	}
	return nil
}

func (a *Adapter) handleRoute(request satoriserver.Request[any]) (any, error) {
	account := a.Account()
	if account == nil {
		return nil, satoriserver.NotFound("no account found")
	}

	if request.Action == string(client.ApiUploadCreate) {
		if !a.postUpload {
			return nil, satoriserver.NotFound("upload.create is not enabled")
		}
		form, ok := request.Params.(*multipart.Form)
		if !ok || form == nil {
			return nil, satoriserver.BadRequest("invalid multipart form")
		}
		uploads, err := formToUploads(form)
		if err != nil {
			return nil, err
		}
		return account.Protocol.UploadCreateNamed(requestContext(request.Origin), uploads)
	}

	params, err := paramsToObject(request.Params)
	if err != nil {
		return nil, err
	}

	raw, err := account.Protocol.CallAPI(
		requestContext(request.Origin),
		request.Action,
		params,
		false,
		requestMethod(request.Origin),
	)
	if err != nil {
		return nil, err
	}
	if len(bytes.TrimSpace(raw)) == 0 {
		return map[string]any{}, nil
	}

	var result any
	if err := json.Unmarshal(raw, &result); err != nil {
		return nil, err
	}
	return result, nil
}

func (a *Adapter) fetchURL(ctx context.Context, rawURL string) (*satoriserver.Response, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, err
	}
	resp, err := a.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	result := satoriserver.NewResponse(resp.StatusCode, body)
	for key, values := range resp.Header {
		for _, value := range values {
			result.Header.Add(key, value)
		}
	}
	return result, nil
}

func requestContext(request *http.Request) context.Context {
	if request == nil {
		return context.Background()
	}
	return request.Context()
}

func requestMethod(request *http.Request) string {
	if request == nil || strings.TrimSpace(request.Method) == "" {
		return http.MethodPost
	}
	return request.Method
}

func paramsToObject(raw any) (map[string]any, error) {
	if raw == nil {
		return map[string]any{}, nil
	}
	params, ok := raw.(map[string]any)
	if !ok {
		return nil, satoriserver.BadRequest("request params must be an object")
	}
	return params, nil
}

func formToUploads(form *multipart.Form) (map[string]client.Upload, error) {
	result := map[string]client.Upload{}
	if form == nil {
		return result, nil
	}
	for name, files := range form.File {
		if len(files) == 0 {
			continue
		}
		fileHeader := files[0]
		opened, err := fileHeader.Open()
		if err != nil {
			return nil, err
		}
		data, err := io.ReadAll(opened)
		_ = opened.Close()
		if err != nil {
			return nil, err
		}
		result[name] = client.NewUpload(
			data,
			fileHeader.Filename,
			fileHeader.Header.Get("Content-Type"),
		)
	}
	return result, nil
}

var supportedAPIActions = []string{
	string(client.ApiMessageCreate),
	string(client.ApiMessageUpdate),
	string(client.ApiMessageGet),
	string(client.ApiMessageDelete),
	string(client.ApiMessageList),
	string(client.ApiChannelGet),
	string(client.ApiChannelList),
	string(client.ApiChannelCreate),
	string(client.ApiChannelUpdate),
	string(client.ApiChannelDelete),
	string(client.ApiChannelMute),
	string(client.ApiUserChannelCreate),
	string(client.ApiGuildGet),
	string(client.ApiGuildList),
	string(client.ApiGuildApprove),
	string(client.ApiGuildMemberList),
	string(client.ApiGuildMemberGet),
	string(client.ApiGuildMemberKick),
	string(client.ApiGuildMemberMute),
	string(client.ApiGuildMemberApprove),
	string(client.ApiGuildMemberRoleSet),
	string(client.ApiGuildMemberRoleUnset),
	string(client.ApiGuildRoleList),
	string(client.ApiGuildRoleCreate),
	string(client.ApiGuildRoleUpdate),
	string(client.ApiGuildRoleDelete),
	string(client.ApiReactionCreate),
	string(client.ApiReactionDelete),
	string(client.ApiReactionClear),
	string(client.ApiReactionList),
	string(client.ApiLoginGet),
	string(client.ApiUserGet),
	string(client.ApiFriendList),
	string(client.ApiFriendApprove),
	string(client.ApiUploadCreate),
}

var _ satoriserver.Adapter = (*Adapter)(nil)
var _ satoriserver.EventPublisher = (*Adapter)(nil)
var _ satoriserver.ServerAware = (*Adapter)(nil)
