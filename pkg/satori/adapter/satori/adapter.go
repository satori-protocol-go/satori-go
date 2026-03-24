package satori

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"strings"
	"time"

	"github.com/satori-protocol-go/satori-go/pkg/satori/client"
	"github.com/satori-protocol-go/satori-go/pkg/satori/model/event"
	"github.com/satori-protocol-go/satori-go/pkg/satori/model/login"
	"github.com/satori-protocol-go/satori-go/pkg/satori/protocol"
	"github.com/satori-protocol-go/satori-go/pkg/satori/server"
)

const defaultEventBuffer = 128

type Config struct {
	Host             string
	Port             int
	Path             string
	Version          string
	Token            string
	Secure           bool
	Timeout          time.Duration
	HandshakeTimeout time.Duration
	PostUpload       bool

	EventBuffer int
	HTTPClient  *http.Client
}

type Adapter struct {
	server.RouterMixin

	app        *client.App
	postUpload bool
	httpClient *http.Client
	eventCh    chan *event.Event
}

func New(cfg Config) (*Adapter, error) {
	app, err := client.NewApp(client.WebSocketConfig{
		Host:             cfg.Host,
		Port:             cfg.Port,
		Path:             cfg.Path,
		Version:          cfg.Version,
		Token:            cfg.Token,
		Secure:           cfg.Secure,
		Timeout:          cfg.Timeout,
		HandshakeTimeout: cfg.HandshakeTimeout,
	})
	if err != nil {
		return nil, err
	}

	buffer := cfg.EventBuffer
	if buffer <= 0 {
		buffer = defaultEventBuffer
	}
	httpClient := cfg.HTTPClient
	if httpClient == nil {
		httpClient = &http.Client{Timeout: protocol.DefaultRequestTimeout}
	}

	adapter := &Adapter{
		app:        app,
		postUpload: cfg.PostUpload,
		httpClient: httpClient,
		eventCh:    make(chan *event.Event, buffer),
	}

	adapter.Route(protocol.ParseApi("internal/*"), adapter.handleRoute)
	for _, api := range protocol.AllApis() {
		if !cfg.PostUpload && api == protocol.ApiUploadCreate {
			continue
		}
		adapter.Route(api, adapter.handleRoute)
	}

	adapter.app.Register(func(_ *client.Account, evt *event.Event) error {
		if evt == nil {
			return nil
		}
		select {
		case adapter.eventCh <- evt:
		default:
			// Keep client network loops responsive when consumers are slow.
		}
		return nil
	})

	return adapter, nil
}

func (a *Adapter) EnsureServer(_ *server.Server) {}

func (a *Adapter) Publisher(_ context.Context) <-chan *event.Event {
	return a.eventCh
}

func (a *Adapter) Block(ctx context.Context) error {
	if err := a.app.Run(ctx); err != nil && !errors.Is(err, context.Canceled) {
		return err
	}
	return nil
}

func (a *Adapter) Cleanup(_ context.Context) error {
	return a.app.Close()
}

func (a *Adapter) GetLogins(_ context.Context) ([]*login.Login, error) {
	account := a.Account()
	if account == nil || account.SelfInfo == nil {
		return []*login.Login{}, nil
	}
	return []*login.Login{cloneLogin(account.SelfInfo)}, nil
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

func (a *Adapter) HandleInternal(request server.Request[map[string]any], path string) (*server.Response, error) {
	path = strings.TrimSpace(path)
	if strings.HasPrefix(path, "_api") {
		account := a.Account()
		if account == nil {
			return nil, server.NotFound("no account found")
		}

		action := strings.TrimPrefix(path, "_api")
		action = strings.TrimPrefix(action, "/")
		if strings.TrimSpace(action) == "" {
			return nil, server.BadRequest("internal api action is required")
		}

		params, err := internalCallParams(request)
		if err != nil {
			return nil, err
		}
		method := requestMethod(request.Origin, http.MethodPost)
		raw, err := account.Protocol.CallAPI(requestContext(request.Origin), action, params, false, method)
		if err != nil {
			return nil, err
		}

		resp := server.NewResponse(http.StatusOK, raw)
		resp.Header.Set("Content-Type", "application/json")
		return resp, nil
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
		return server.NewResponse(http.StatusOK, data), nil
	}

	if path == "" {
		return nil, server.NotFound("path is empty")
	}
	return a.fetchURL(requestContext(request.Origin), path)
}

func (a *Adapter) HandleProxied(_ string, rawURL string) (*server.Response, error) {
	return a.fetchURL(context.Background(), rawURL)
}

func (a *Adapter) Account() *client.Account {
	accounts := a.app.Accounts()
	for _, account := range accounts {
		return account
	}
	return nil
}

func (a *Adapter) handleRoute(request *server.Request[any]) (any, error) {
	if request == nil {
		return nil, server.BadRequest("request cannot be nil")
	}

	account := a.Account()
	if account == nil {
		return nil, server.NotFound("no account found")
	}

	method := http.MethodPost
	if request.Action == string(protocol.ApiUploadCreate) {
		if !a.postUpload {
			return nil, server.NotFound("upload.create is not enabled")
		}
		form, ok := request.Params.(*multipart.Form)
		if !ok || form == nil {
			return nil, server.BadRequest("invalid multipart form")
		}
		params, err := formToMultipartParams(form)
		if err != nil {
			return nil, err
		}
		raw, err := account.Protocol.CallAPI(requestContext(request.Origin), request.Action, params, true, method)
		if err != nil {
			return nil, err
		}
		return decodeRawResult(raw)
	}

	params, err := paramsToObject(request.Params)
	if err != nil {
		return nil, err
	}
	raw, err := account.Protocol.CallAPI(requestContext(request.Origin), request.Action, params, false, method)
	if err != nil {
		return nil, err
	}
	return decodeRawResult(raw)
}

func (a *Adapter) fetchURL(ctx context.Context, rawURL string) (*server.Response, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, err
	}
	resp, err := a.httpClient.Do(request)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	result := server.NewResponse(resp.StatusCode, body)
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

func requestMethod(request *http.Request, fallback string) string {
	if request == nil || strings.TrimSpace(request.Method) == "" {
		return fallback
	}
	return request.Method
}

func paramsToObject(raw any) (map[string]any, error) {
	if raw == nil {
		return map[string]any{}, nil
	}
	params, ok := raw.(map[string]any)
	if !ok {
		return nil, server.BadRequest("request params must be an object")
	}
	return params, nil
}

func decodeRawResult(raw []byte) (any, error) {
	if len(bytes.TrimSpace(raw)) == 0 {
		return map[string]any{}, nil
	}
	var result any
	if err := json.Unmarshal(raw, &result); err != nil {
		return nil, err
	}
	return result, nil
}

func formToMultipartParams(form *multipart.Form) (map[string]any, error) {
	result := map[string]any{}
	if form == nil {
		return result, nil
	}
	for key, values := range form.Value {
		if len(values) == 0 {
			continue
		}
		result[key] = values[len(values)-1]
	}
	for name, files := range form.File {
		if len(files) == 0 {
			continue
		}
		fileHeader := files[len(files)-1]
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

func internalCallParams(request server.Request[map[string]any]) (map[string]any, error) {
	params := map[string]any{}
	if request.Params != nil {
		params = request.Params
	}
	if request.Origin == nil {
		return params, nil
	}

	body, err := io.ReadAll(request.Origin.Body)
	if err != nil {
		return nil, err
	}
	request.Origin.Body = io.NopCloser(bytes.NewReader(body))
	if len(bytes.TrimSpace(body)) == 0 {
		return params, nil
	}

	decoded := map[string]any{}
	if err := json.Unmarshal(body, &decoded); err != nil {
		return nil, server.BadRequest("invalid internal api params")
	}
	return decoded, nil
}

func cloneLogin(source *login.Login) *login.Login {
	if source == nil {
		return nil
	}
	cloned := *source
	if source.User != nil {
		userValue := *source.User
		cloned.User = &userValue
	}
	cloned.Features = append([]string(nil), source.Features...)
	return &cloned
}

var _ server.Adapter = (*Adapter)(nil)
var _ server.EventPublisher = (*Adapter)(nil)
var _ server.Blockable = (*Adapter)(nil)
var _ server.Cleanable = (*Adapter)(nil)
