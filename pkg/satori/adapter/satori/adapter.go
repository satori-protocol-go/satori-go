package satori

import (
	"context"
	"errors"
	"io"
	"mime/multipart"
	"net/http"
	"net/url"
	"sort"
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
	EventBuffer      int
	HTTPClient       *http.Client
}

type Adapter struct {
	server.RouterMixin
	app          *client.App
	postUpload   bool
	httpClient   *http.Client
	eventCh      chan *event.Event
	eventContext context.Context
	cancelEvents context.CancelFunc
}

func New(cfg Config) (*Adapter, error) {
	app, err := client.NewApp(client.WebSocketConfig{
		Host: cfg.Host, Port: cfg.Port, Path: cfg.Path, Version: cfg.Version,
		Token: cfg.Token, Secure: cfg.Secure, Timeout: cfg.Timeout, HandshakeTimeout: cfg.HandshakeTimeout,
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
	ctx, cancel := context.WithCancel(context.Background())
	adapter := &Adapter{app: app, postUpload: cfg.PostUpload, httpClient: httpClient,
		eventCh: make(chan *event.Event, buffer), eventContext: ctx, cancelEvents: cancel}
	app.SetDefaultProtocolFactory(func(account *client.Account) *client.APIProtocol {
		return client.NewAPIProtocol(account, httpClient)
	})
	adapter.Route(protocol.ParseApi("internal/*"), adapter.handleRoute)
	for _, api := range protocol.AllApis() {
		if !cfg.PostUpload && api == protocol.ApiUploadCreate {
			continue
		}
		adapter.Route(api, adapter.handleRoute)
	}
	app.Register(func(_ *client.Account, evt *event.Event) error {
		if evt == nil {
			return nil
		}
		select {
		case adapter.eventCh <- evt:
			return nil
		case <-adapter.eventContext.Done():
			return adapter.eventContext.Err()
		}
	})
	return adapter, nil
}

func (a *Adapter) EnsureServer(_ *server.Server)                   {}
func (a *Adapter) Publisher(_ context.Context) <-chan *event.Event { return a.eventCh }

func (a *Adapter) Block(ctx context.Context) error {
	stop := context.AfterFunc(ctx, a.cancelEvents)
	defer stop()
	defer a.cancelEvents()
	if err := a.app.Run(ctx); err != nil && !errors.Is(err, context.Canceled) {
		return err
	}
	return nil
}

func (a *Adapter) Cleanup(_ context.Context) error {
	a.cancelEvents()
	return a.app.Close()
}

func (a *Adapter) GetLogins(_ context.Context) ([]*login.Login, error) {
	result := make([]*login.Login, 0)
	targets := map[string]bool{}
	for _, account := range a.app.Accounts() {
		info := account.SelfInfo()
		if info == nil {
			continue
		}
		if info.User != nil && info.Platform != "" {
			key := info.Platform + "/" + info.User.Id
			if targets[key] {
				return nil, server.NewActionError(409, "ambiguous upstream account "+key, nil)
			}
			targets[key] = true
		}
		result = append(result, info)
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Sn < result[j].Sn })
	return result, nil
}

func (a *Adapter) ProxyUrls() []string {
	set := map[string]bool{}
	for _, account := range a.app.Accounts() {
		for _, prefix := range account.ProxyURLs() {
			set[prefix] = true
		}
	}
	result := make([]string, 0, len(set))
	for prefix := range set {
		result = append(result, prefix)
	}
	sort.Strings(result)
	return result
}

func (a *Adapter) Ensure(platform, selfID string) bool {
	for _, account := range a.app.Accounts() {
		if account.Platform() == platform && account.SelfID() == selfID {
			return true
		}
	}
	return false
}

// Account resolves exactly the requested platform identity within this upstream.
func (a *Adapter) Account(platform, selfID string) (*client.Account, error) {
	var selected *client.Account
	for _, account := range a.app.Accounts() {
		if account.Platform() != platform || account.SelfID() != selfID {
			continue
		}
		if selected != nil {
			return nil, server.NewActionError(409, "ambiguous upstream account", nil)
		}
		selected = account
	}
	if selected == nil {
		return nil, server.NotFound("upstream account not found")
	}
	return selected, nil
}

func (a *Adapter) HandleInternal(request server.Request[map[string]any], path string) (*server.Response, error) {
	account, err := a.Account(request.Platform, request.SelfID)
	if err != nil {
		return nil, err
	}
	if path == "_api" {
		return nil, server.BadRequest("internal API action is required")
	}
	if strings.HasPrefix(path, "_api/") {
		endpoint := strings.TrimRight(account.Config().APIBase(), "/") + "/internal/" + strings.TrimPrefix(path, "_api/")
		return a.forwardNative(account, request.Origin, endpoint)
	}
	endpoint := "internal:" + request.Platform + "/" + request.SelfID + "/" + path
	return a.forwardNative(account, request.Origin, endpoint)
}

func (a *Adapter) HandleProxied(ctx context.Context, _ string, rawURL string) (*server.Response, error) {
	parsed, err := url.Parse(rawURL)
	if err != nil || parsed.Host == "" || parsed.User != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") {
		return nil, server.BadRequest("invalid resource URL")
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, err
	}
	response, err := a.httpClient.Do(request)
	if err != nil {
		return nil, err
	}
	return forwardedResponse(response), nil
}

func (a *Adapter) handleRoute(request *server.Request[any]) (any, error) {
	if request == nil {
		return nil, server.BadRequest("request is required")
	}
	account, err := a.Account(request.Platform, request.SelfID)
	if err != nil {
		return nil, err
	}
	if strings.HasPrefix(request.Action, protocol.InternalApiPrefix) {
		endpoint := strings.TrimRight(account.Config().APIBase(), "/") + "/" + request.Action
		return a.forwardNative(account, request.Origin, endpoint)
	}
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
		return account.Protocol.CallAPI(requestContext(request.Origin), request.Action, params, true, http.MethodPost)
	}
	params, ok := request.Params.(map[string]any)
	if request.Params != nil && !ok {
		return nil, server.BadRequest("request params must be an object")
	}
	return account.Protocol.CallAPI(requestContext(request.Origin), request.Action, params, false, http.MethodPost)
}

func (a *Adapter) forwardNative(account *client.Account, origin *http.Request, endpoint string) (*server.Response, error) {
	method := http.MethodGet
	var options []client.RequestOption
	if origin != nil {
		method = origin.Method
		options = append(options, client.WithRequestBody(origin.Body, origin.Header.Get("Content-Type")))
		if origin.URL.RawQuery != "" && !strings.Contains(endpoint, "?") {
			endpoint += "?" + origin.URL.RawQuery
		}
		for _, key := range []string{"Accept", "Range", "If-None-Match", "If-Modified-Since"} {
			if value := origin.Header.Get(key); value != "" {
				options = append(options, client.WithRequestHeader(key, value))
			}
		}
	}
	response, err := account.Protocol.RequestInternal(requestContext(origin), endpoint, method, nil, options...)
	if err != nil {
		return nil, err
	}
	return forwardedResponse(response), nil
}

func forwardedResponse(response *http.Response) *server.Response {
	result := server.NewStreamResponse(response.StatusCode, response.Body)
	result.ContentLength = response.ContentLength
	result.Header = response.Header.Clone()
	return result
}

func requestContext(request *http.Request) context.Context {
	if request == nil {
		return context.Background()
	}
	return request.Context()
}

func formToMultipartParams(form *multipart.Form) (map[string]any, error) {
	result := map[string]any{}
	for key, values := range form.Value {
		if len(values) > 0 {
			result[key] = values[len(values)-1]
		}
	}
	for name, files := range form.File {
		if len(files) != 1 {
			return nil, server.BadRequest("upload field names must be unique")
		}
		file := files[0]
		opened, err := file.Open()
		if err != nil {
			return nil, err
		}
		data, err := io.ReadAll(opened)
		opened.Close()
		if err != nil {
			return nil, err
		}
		result[name] = client.NewUpload(data, file.Filename, file.Header.Get("Content-Type"))
	}
	return result, nil
}

var _ server.Adapter = (*Adapter)(nil)
var _ server.EventPublisher = (*Adapter)(nil)
var _ server.Blockable = (*Adapter)(nil)
var _ server.Cleanable = (*Adapter)(nil)
