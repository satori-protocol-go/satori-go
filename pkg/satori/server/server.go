package server

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	"github.com/satori-protocol-go/satori-go/pkg/satori/model/event"
	"github.com/satori-protocol-go/satori-go/pkg/satori/model/login"
	"github.com/satori-protocol-go/satori-go/pkg/satori/model/meta"
	"github.com/satori-protocol-go/satori-go/pkg/satori/model/operation"
)

const (
	defaultHost            = "127.0.0.1"
	defaultPort            = 5140
	defaultVersion         = "v1"
	defaultEventCacheSize  = 100
	defaultStreamThreshold = 16 * 1024 * 1024
	defaultStreamChunkSize = 64 * 1024
	defaultHeartbeat       = 12 * time.Second
	defaultIdentifyTimeout = 30 * time.Second
	defaultReadFormMemory  = 32 << 20 // 32 MB
)

var (
	internalURLPattern = regexp.MustCompile(`^internal:([^/]+)/([^/]+)/(.+)$`)
	upgrader           = websocket.Upgrader{
		CheckOrigin: func(*http.Request) bool { return true },
	}
)

type Config struct {
	Host            string
	Port            int
	Path            string
	Version         string
	Token           string
	Webhooks        []WebhookEndpoint
	StreamThreshold int
	StreamChunkSize int
	EventCacheSize  int
	HTTPClient      *http.Client
}

type Server struct {
	Host    string
	Port    int
	Path    string
	Version string
	Token   string

	streamThreshold int
	streamChunkSize int

	mu          sync.RWMutex
	routes      map[string]RouteCall
	routers     []Router
	adapters    []Adapter
	providers   []Provider
	resources   map[string]string
	webhooks    []WebhookEndpoint
	connections map[*websocketConnection]struct{}

	sequence   int64
	eventCache eventDeque

	tempDir string

	httpClient *http.Client
	httpServer *http.Server
}

func NewServer(cfg Config) (*Server, error) {
	host := strings.TrimSpace(cfg.Host)
	if host == "" {
		host = defaultHost
	}

	port := cfg.Port
	if port == 0 {
		port = defaultPort
	}

	version := strings.TrimSpace(cfg.Version)
	if version == "" {
		version = defaultVersion
	}

	path := strings.TrimSpace(cfg.Path)
	if path != "" && !strings.HasPrefix(path, "/") {
		path = "/" + path
	}
	path = strings.TrimSuffix(path, "/")

	if (host == "0.0.0.0" || host == "::") && strings.TrimSpace(cfg.Token) == "" {
		return nil, errors.New("token is required when host is exposed to the public network")
	}

	streamThreshold := cfg.StreamThreshold
	if streamThreshold <= 0 {
		streamThreshold = defaultStreamThreshold
	}
	streamChunkSize := cfg.StreamChunkSize
	if streamChunkSize <= 0 {
		streamChunkSize = defaultStreamChunkSize
	}
	eventCacheSize := cfg.EventCacheSize
	if eventCacheSize <= 0 {
		eventCacheSize = defaultEventCacheSize
	}

	tempDir, err := os.MkdirTemp("", "satori-server-*")
	if err != nil {
		return nil, err
	}

	httpClient := cfg.HTTPClient
	if httpClient == nil {
		httpClient = &http.Client{}
	}

	server := &Server{
		Host:            host,
		Port:            port,
		Path:            path,
		Version:         version,
		Token:           cfg.Token,
		streamThreshold: streamThreshold,
		streamChunkSize: streamChunkSize,
		routes:          map[string]RouteCall{},
		resources:       map[string]string{},
		connections:     map[*websocketConnection]struct{}{},
		webhooks:        append([]WebhookEndpoint(nil), cfg.Webhooks...),
		eventCache:      newEventDeque(eventCacheSize),
		tempDir:         tempDir,
		httpClient:      httpClient,
	}

	return server, nil
}

func (s *Server) URLBase() string {
	return fmt.Sprintf("http://%s:%d%s/%s", s.Host, s.Port, s.Path, s.Version)
}

func (s *Server) Apply(item any) error {
	switch typed := item.(type) {
	case Adapter:
		if aware, ok := any(typed).(ServerAware); ok {
			aware.EnsureServer(s)
		}
		s.mu.Lock()
		s.adapters = append(s.adapters, typed)
		s.providers = append(s.providers, typed)
		s.mu.Unlock()
		return nil
	case Provider:
		s.mu.Lock()
		s.providers = append(s.providers, typed)
		s.mu.Unlock()
		return nil
	case Router:
		s.mu.Lock()
		s.routers = append(s.routers, typed)
		s.mu.Unlock()
		return nil
	default:
		return fmt.Errorf("unknown apply type %T", item)
	}
}

func (s *Server) Route(action string, handler RouteCall) error {
	if handler == nil {
		return errors.New("handler cannot be nil")
	}
	normalized := normalizeRouteAction(action)
	if normalized == "" {
		return errors.New("action cannot be empty")
	}
	s.mu.Lock()
	s.routes[normalized] = handler
	s.mu.Unlock()
	return nil
}

func (s *Server) Mount(routePath string, filePath string) error {
	routePath = strings.TrimSpace(routePath)
	filePath = strings.TrimSpace(filePath)
	if routePath == "" || filePath == "" {
		return errors.New("routePath and filePath cannot be empty")
	}
	if !strings.HasPrefix(routePath, "/") {
		routePath = "/" + routePath
	}
	s.mu.Lock()
	s.resources[routePath] = filePath
	s.mu.Unlock()
	return nil
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	base := s.apiBasePath()

	mux.HandleFunc(base+"/events", s.websocketServerHandler)
	mux.HandleFunc(base+"/meta", s.metaGetHandler)
	mux.HandleFunc(base+"/meta/webhook.create", s.webhookCreateHandler)
	mux.HandleFunc(base+"/meta/webhook.delete", s.webhookDeleteHandler)
	mux.HandleFunc(base+"/proxy/", s.proxyURLHandler)
	mux.HandleFunc(base+"/", s.httpServerHandler)

	s.mountResources(mux)
	s.mountAdapterRootRoutes(mux)
	return mux
}

func (s *Server) Run(ctx context.Context) error {
	if ctx == nil {
		ctx = context.Background()
	}

	if err := s.broadcastMetaToWebhooks(ctx); err != nil {
		return err
	}

	server := &http.Server{
		Addr:    fmt.Sprintf("%s:%d", s.Host, s.Port),
		Handler: s.Handler(),
	}

	s.mu.Lock()
	s.httpServer = server
	s.mu.Unlock()

	publishCtx, cancelPublish := context.WithCancel(ctx)
	defer cancelPublish()

	var publisherWG sync.WaitGroup
	for _, provider := range s.snapshotProviders() {
		publisher, ok := provider.(EventPublisher)
		if !ok {
			continue
		}
		publisherWG.Add(1)
		go func(stream <-chan *event.Event) {
			defer publisherWG.Done()
			for {
				select {
				case <-publishCtx.Done():
					return
				case evt, ok := <-stream:
					if !ok {
						return
					}
					_ = s.Post(evt)
				}
			}
		}(publisher.Publisher(publishCtx))
	}

	errCh := make(chan error, 1)
	go func() {
		if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
		}
		close(errCh)
	}()

	select {
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = s.Shutdown(shutdownCtx)
		cancelPublish()
		publisherWG.Wait()
		return nil
	case err := <-errCh:
		cancelPublish()
		publisherWG.Wait()
		return err
	}
}

func (s *Server) Shutdown(ctx context.Context) error {
	s.mu.Lock()
	server := s.httpServer
	s.httpServer = nil

	connections := make([]*websocketConnection, 0, len(s.connections))
	for connection := range s.connections {
		connections = append(connections, connection)
	}
	s.connections = map[*websocketConnection]struct{}{}

	tempDir := s.tempDir
	s.tempDir = ""
	s.mu.Unlock()

	for _, connection := range connections {
		_ = connection.Close()
	}

	var shutdownErr error
	if server != nil {
		shutdownErr = server.Shutdown(ctx)
	}

	if tempDir != "" {
		_ = os.RemoveAll(tempDir)
	}
	return shutdownErr
}

func (s *Server) Close() error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	return s.Shutdown(ctx)
}

func (s *Server) Post(evt *event.Event) error {
	if evt == nil {
		return nil
	}

	s.mu.Lock()
	evt.Sn = s.sequence
	s.sequence++
	s.eventCache.Append(evt)

	connections := make([]*websocketConnection, 0, len(s.connections))
	for connection := range s.connections {
		connections = append(connections, connection)
	}
	webhooks := append([]WebhookEndpoint(nil), s.webhooks...)
	s.mu.Unlock()

	payload := map[string]any{"op": operation.OpcodeEvent, "body": evt}
	for _, connection := range connections {
		if !connection.Alive() {
			continue
		}
		if err := connection.Send(payload); err != nil {
			_ = connection.Close()
			s.removeConnection(connection)
		}
	}

	for _, webhook := range webhooks {
		_ = s.sendWebhook(webhook, operation.OpcodeEvent, evt)
	}
	return nil
}

func (s *Server) GetLocalFile(rawURL string) ([]byte, error) {
	name := filepath.Base(rawURL)
	if name == "." || name == string(filepath.Separator) {
		return nil, os.ErrNotExist
	}
	s.mu.RLock()
	tempDir := s.tempDir
	s.mu.RUnlock()
	if tempDir == "" {
		return nil, os.ErrNotExist
	}
	return os.ReadFile(filepath.Join(tempDir, name))
}

func (s *Server) metaGetHandler(w http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodPost {
		writeMethodNotAllowed(w)
		return
	}

	logins, proxyUrls, err := s.collectMeta(request.Context())
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, meta.Meta{
		Logins:    logins,
		ProxyUrls: proxyUrls,
	})
}

func (s *Server) webhookCreateHandler(w http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodPost {
		writeMethodNotAllowed(w)
		return
	}

	var payload struct {
		URL     string   `json:"url"`
		Token   string   `json:"token"`
		Timeout *float64 `json:"timeout"`
	}
	if err := decodeJSONBody(request.Body, &payload); err != nil {
		writeError(w, BadRequest(err.Error()))
		return
	}
	if strings.TrimSpace(payload.URL) == "" {
		writeError(w, BadRequest("url is required"))
		return
	}

	hook := WebhookEndpoint{
		URL:   payload.URL,
		Token: payload.Token,
	}
	if payload.Timeout != nil && *payload.Timeout > 0 {
		hook.Timeout = time.Duration(*payload.Timeout * float64(time.Second))
	}

	s.mu.Lock()
	s.webhooks = append(s.webhooks, hook)
	s.mu.Unlock()

	_, proxyUrls, err := s.collectMeta(request.Context())
	if err != nil {
		writeError(w, err)
		return
	}
	if err := s.sendWebhook(hook, operation.OpcodeMeta, map[string]any{"proxy_urls": proxyUrls}); err != nil {
		writeError(w, err)
		return
	}
	w.WriteHeader(http.StatusOK)
}

func (s *Server) webhookDeleteHandler(w http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodPost {
		writeMethodNotAllowed(w)
		return
	}

	var payload struct {
		URL string `json:"url"`
	}
	if err := decodeJSONBody(request.Body, &payload); err != nil {
		writeError(w, BadRequest(err.Error()))
		return
	}

	s.mu.Lock()
	filtered := s.webhooks[:0]
	for _, endpoint := range s.webhooks {
		if endpoint.URL == payload.URL {
			continue
		}
		filtered = append(filtered, endpoint)
	}
	s.webhooks = filtered
	s.mu.Unlock()
	w.WriteHeader(http.StatusOK)
}

func (s *Server) websocketServerHandler(w http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodGet {
		writeMethodNotAllowed(w)
		return
	}

	conn, err := upgrader.Upgrade(w, request, nil)
	if err != nil {
		return
	}
	connection := newWebsocketConnection(conn)
	defer connection.Close()

	token, sequence, err := readIdentify(connection)
	if err != nil {
		_ = connection.CloseWith(3000, "Unauthorized")
		return
	}
	if token != s.Token {
		_ = connection.CloseWith(3000, "Unauthorized")
		return
	}

	logins, proxyUrls, err := s.collectMeta(request.Context())
	if err != nil {
		_ = connection.CloseWith(websocket.CloseInternalServerErr, "Internal Server Error")
		return
	}

	if err := connection.Send(map[string]any{
		"op": operation.OpcodeReady,
		"body": map[string]any{
			"logins":     logins,
			"proxy_urls": proxyUrls,
		},
	}); err != nil {
		return
	}

	s.addConnection(connection)
	defer s.removeConnection(connection)

	if sequence > -1 {
		for _, evt := range s.eventCache.After(sequence) {
			if evt == nil || isLoginEventType(evt.Type) {
				continue
			}
			if err := connection.Send(map[string]any{
				"op":   operation.OpcodeEvent,
				"body": evt,
			}); err != nil {
				return
			}
			time.Sleep(100 * time.Millisecond)
		}
	}

	connection.Heartbeat(defaultHeartbeat)
}

func (s *Server) httpServerHandler(w http.ResponseWriter, request *http.Request) {
	action := s.extractAction(request)
	if action == "" {
		writeError(w, NotFound("action not found"))
		return
	}

	platform, selfID, err := extractPlatformAndSelfID(request.Header)
	if err != nil {
		writeError(w, err)
		return
	}

	handler, ok := s.findRouteHandler(action, platform, selfID)
	if !ok {
		writeError(w, NotFound(fmt.Sprintf(
			"Action %q is not supported in current platform %q.",
			action,
			platform,
		)))
		return
	}

	s.executeRoute(w, request, action, platform, selfID, handler)
}

func (s *Server) proxyURLHandler(w http.ResponseWriter, request *http.Request) {
	base := s.apiBasePath() + "/proxy/"
	rawURL := strings.TrimPrefix(request.URL.Path, base)
	if rawURL == "" {
		writeError(w, NotFound("proxy target is empty"))
		return
	}

	resp, err := s.fetchProxy(rawURL, request)
	if err != nil {
		writeError(w, err)
		return
	}

	if resp == nil {
		writeError(w, NewActionFailed(http.StatusInternalServerError, "empty proxy response", nil))
		return
	}

	if len(resp.Body) > s.streamThreshold {
		writeStreamResponse(w, resp, s.streamChunkSize)
		return
	}
	writeServerResponse(w, resp)
}

func (s *Server) executeRoute(
	w http.ResponseWriter,
	request *http.Request,
	action string,
	platform string,
	selfID string,
	handler RouteCall,
) {
	params, err := parseParams(action, request)
	if err != nil {
		writeError(w, err)
		return
	}

	result, callErr := handler(Request[any]{
		Origin:   request,
		Action:   action,
		Params:   params,
		Platform: platform,
		SelfID:   selfID,
	})
	if callErr != nil {
		writeError(w, callErr)
		return
	}

	switch typed := result.(type) {
	case *Response:
		writeServerResponse(w, typed)
	case Response:
		writeServerResponse(w, &typed)
	default:
		writeJSON(w, http.StatusOK, typed)
	}
}

func (s *Server) findRouteHandler(action string, platform string, selfID string) (RouteCall, bool) {
	s.mu.RLock()
	adapters := append([]Adapter(nil), s.adapters...)
	serverRoutes := copyRouteMap(s.routes)
	routers := append([]Router(nil), s.routers...)
	s.mu.RUnlock()

	for _, adapter := range adapters {
		handler, ok := matchRoute(adapter.Routes(), action)
		if !ok {
			continue
		}
		if !adapter.Ensure(platform, selfID) {
			continue
		}
		return handler, true
	}

	if handler, ok := matchRoute(serverRoutes, action); ok {
		return handler, true
	}

	for _, router := range routers {
		if handler, ok := matchRoute(router.Routes(), action); ok {
			return handler, true
		}
	}

	if action == string(ApiUploadCreate) {
		return s.defaultUploadCreateHandler, true
	}

	return nil, false
}

func (s *Server) fetchProxy(rawURL string, request *http.Request) (*Response, error) {
	normalized, err := normalizeProxyURL(rawURL)
	if err != nil {
		return nil, BadRequest(err.Error())
	}

	if strings.HasPrefix(normalized, "internal:") {
		return s.fetchInternalProxy(normalized, request)
	}
	return s.fetchExternalProxy(normalized)
}

func (s *Server) fetchInternalProxy(rawURL string, request *http.Request) (*Response, error) {
	match := internalURLPattern.FindStringSubmatch(rawURL)
	if len(match) != 4 {
		return nil, BadRequest(fmt.Sprintf("invalid internal url: %s", rawURL))
	}

	platform := match[1]
	selfID := match[2]
	path := match[3]

	if strings.HasPrefix(path, "_tmp/") {
		return s.fetchTempFile(path[5:])
	}

	if request == nil {
		return nil, NotFound("request context is required for internal proxy")
	}

	for _, provider := range s.snapshotProviders() {
		if !provider.Ensure(platform, selfID) {
			continue
		}
		resp, err := provider.HandleInternal(Request[map[string]any]{
			Origin:   request,
			Action:   "internal",
			Params:   map[string]any{},
			Platform: platform,
			SelfID:   selfID,
		}, path)
		if err != nil {
			return nil, err
		}
		if resp != nil {
			return resp, nil
		}
	}

	return nil, NotFound(fmt.Sprintf("login with %s:%s not found", platform, selfID))
}

func (s *Server) fetchExternalProxy(rawURL string) (*Response, error) {
	for _, provider := range s.snapshotProviders() {
		for _, prefix := range provider.ProxyUrls() {
			if !strings.HasPrefix(rawURL, prefix) {
				continue
			}
			resp, err := provider.HandleProxied(prefix, rawURL)
			if err != nil {
				return nil, err
			}
			if resp != nil {
				return resp, nil
			}
		}
	}
	return nil, Forbidden(fmt.Sprintf("unknown proxy url: %s", rawURL))
}

func (s *Server) fetchTempFile(name string) (*Response, error) {
	s.mu.RLock()
	tempDir := s.tempDir
	s.mu.RUnlock()
	if tempDir == "" {
		return nil, NotFound("temporary directory is not available")
	}

	cleanName := filepath.Clean(name)
	if strings.HasPrefix(cleanName, "..") {
		return nil, BadRequest("invalid file path")
	}

	filePath := filepath.Join(tempDir, cleanName)
	data, err := os.ReadFile(filePath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, NotFound(fmt.Sprintf("file not found: %s", cleanName))
		}
		return nil, err
	}

	contentType := mime.TypeByExtension(filepath.Ext(filePath))
	if contentType == "" {
		contentType = "application/octet-stream"
	}
	resp := NewResponse(http.StatusOK, data)
	resp.Header.Set("Content-Type", contentType)
	return resp, nil
}

func (s *Server) defaultUploadCreateHandler(request Request[any]) (any, error) {
	form, ok := request.Params.(*multipart.Form)
	if !ok || form == nil {
		return nil, BadRequest("invalid form data")
	}

	result := map[string]string{}

	for name, files := range form.File {
		if len(files) == 0 {
			continue
		}

		file := files[0]
		opened, err := file.Open()
		if err != nil {
			return nil, err
		}

		data, err := io.ReadAll(opened)
		_ = opened.Close()
		if err != nil {
			return nil, err
		}

		token, err := randomToken(16)
		if err != nil {
			return nil, err
		}

		filename := file.Filename
		if filename == "" {
			ext := ".png"
			if contentType := file.Header.Get("Content-Type"); contentType != "" {
				if exts, _ := mime.ExtensionsByType(contentType); len(exts) > 0 {
					ext = exts[0]
				}
			}
			filename = name + ext
		}
		filename = filepath.Base(filename)
		if filename == "." || filename == string(filepath.Separator) {
			filename = name
		}

		finalName := token + "-" + filename

		s.mu.RLock()
		tempDir := s.tempDir
		s.mu.RUnlock()
		if tempDir == "" {
			return nil, errors.New("temporary directory is not available")
		}

		target := filepath.Join(tempDir, finalName)
		if err := os.WriteFile(target, data, 0o600); err != nil {
			return nil, err
		}
		time.AfterFunc(10*time.Minute, func() {
			_ = os.Remove(target)
		})

		result[name] = fmt.Sprintf("internal:%s/%s/_tmp/%s", request.Platform, request.SelfID, finalName)
	}

	return result, nil
}

func (s *Server) collectMeta(ctx context.Context) ([]*login.Login, []string, error) {
	logins := make([]*login.Login, 0)
	proxyUrls := make([]string, 0)

	for _, provider := range s.snapshotProviders() {
		items, err := provider.GetLogins(ctx)
		if err != nil {
			return nil, nil, err
		}
		logins = append(logins, items...)
		proxyUrls = append(proxyUrls, provider.ProxyUrls()...)
	}
	return logins, proxyUrls, nil
}

func (s *Server) broadcastMetaToWebhooks(ctx context.Context) error {
	_, proxyUrls, err := s.collectMeta(ctx)
	if err != nil {
		return err
	}
	s.mu.RLock()
	webhooks := append([]WebhookEndpoint(nil), s.webhooks...)
	s.mu.RUnlock()

	body := map[string]any{"proxy_urls": proxyUrls}
	for _, webhook := range webhooks {
		if err := s.sendWebhook(webhook, operation.OpcodeMeta, body); err != nil {
			return err
		}
	}
	return nil
}

func (s *Server) sendWebhook(webhook WebhookEndpoint, opcode operation.Opcode, body any) error {
	payload, err := json.Marshal(body)
	if err != nil {
		return err
	}

	ctx := context.Background()
	cancel := func() {}
	if webhook.Timeout > 0 {
		ctx, cancel = context.WithTimeout(ctx, webhook.Timeout)
	}
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, webhook.URL, bytes.NewReader(payload))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+webhook.Token)
	req.Header.Set("Satori-OpCode", strconv.Itoa(int(opcode)))

	resp, err := s.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		bodyData, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return fmt.Errorf("webhook response status %d: %s", resp.StatusCode, string(bodyData))
	}
	return nil
}

func (s *Server) apiBasePath() string {
	return s.Path + "/" + s.Version
}

func (s *Server) extractAction(request *http.Request) string {
	prefix := s.apiBasePath() + "/"
	action := strings.TrimPrefix(request.URL.Path, prefix)
	action = strings.TrimPrefix(action, "/")
	return action
}

func (s *Server) addConnection(connection *websocketConnection) {
	s.mu.Lock()
	s.connections[connection] = struct{}{}
	s.mu.Unlock()
}

func (s *Server) removeConnection(connection *websocketConnection) {
	s.mu.Lock()
	delete(s.connections, connection)
	s.mu.Unlock()
}

func (s *Server) snapshotProviders() []Provider {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return append([]Provider(nil), s.providers...)
}

func (s *Server) mountResources(mux *http.ServeMux) {
	s.mu.RLock()
	resources := map[string]string{}
	for routePath, filePath := range s.resources {
		resources[routePath] = filePath
	}
	s.mu.RUnlock()

	for routePath, filePath := range resources {
		pathInfo, err := os.Stat(filePath)
		if err != nil {
			continue
		}
		if pathInfo.IsDir() {
			prefix := routePath
			if !strings.HasSuffix(prefix, "/") {
				prefix += "/"
			}
			mux.Handle(prefix, http.StripPrefix(prefix, http.FileServer(http.Dir(filePath))))
			continue
		}

		target := filePath
		mux.HandleFunc(routePath, func(w http.ResponseWriter, request *http.Request) {
			http.ServeFile(w, request, target)
		})
	}
}

func (s *Server) mountAdapterRootRoutes(mux *http.ServeMux) {
	s.mu.RLock()
	adapters := append([]Adapter(nil), s.adapters...)
	s.mu.RUnlock()

	for _, adapter := range adapters {
		provider, ok := any(adapter).(RootRouteProvider)
		if !ok {
			continue
		}
		for _, route := range provider.RootRoutes() {
			if route.Handler == nil {
				continue
			}
			path := route.Path
			if !strings.HasPrefix(path, "/") {
				path = "/" + path
			}
			handler := route.Handler
			methods := toUpperMethods(route.Methods)
			if len(methods) > 0 {
				handler = methodFilterHandler(handler, methods)
			}
			mux.Handle(path, handler)
		}
	}
}

func readIdentify(connection *websocketConnection) (string, int64, error) {
	connection.connection.SetReadDeadline(time.Now().Add(defaultIdentifyTimeout))
	defer connection.connection.SetReadDeadline(time.Time{})

	_, payload, err := connection.connection.ReadMessage()
	if err != nil {
		return "", -1, err
	}

	var frame struct {
		Op   operation.Opcode `json:"op"`
		Body map[string]any   `json:"body"`
	}
	if err := json.Unmarshal(payload, &frame); err != nil {
		return "", -1, err
	}
	if frame.Op != operation.OpcodeIdentify {
		return "", -1, errors.New("invalid identify opcode")
	}

	token := asString(frame.Body["token"])
	sequence := int64(-1)
	if value, ok := frame.Body["sequence"]; ok {
		if parsed, ok := toInt64(value); ok {
			sequence = parsed
		}
	} else if value, ok := frame.Body["sn"]; ok {
		if parsed, ok := toInt64(value); ok {
			sequence = parsed
		}
	}

	return token, sequence, nil
}

func parseParams(action string, request *http.Request) (any, error) {
	if action == string(ApiUploadCreate) {
		if err := request.ParseMultipartForm(defaultReadFormMemory); err != nil {
			return nil, BadRequest(err.Error())
		}
		return request.MultipartForm, nil
	}

	if request.Method == http.MethodGet {
		params := map[string]any{}
		for key, values := range request.URL.Query() {
			if len(values) == 1 {
				params[key] = values[0]
				continue
			}
			copied := make([]string, len(values))
			copy(copied, values)
			params[key] = copied
		}
		return params, nil
	}

	body, err := io.ReadAll(request.Body)
	if err != nil {
		return nil, err
	}
	if len(bytes.TrimSpace(body)) == 0 {
		return map[string]any{}, nil
	}
	var params any
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.UseNumber()
	if err := decoder.Decode(&params); err != nil {
		return nil, BadRequest(err.Error())
	}
	return params, nil
}

func normalizeProxyURL(rawURL string) (string, error) {
	rawURL = strings.Replace(rawURL, ":/", "://", 1)
	rawURL = strings.Replace(rawURL, ":///", "://", 1)
	decoded, err := url.PathUnescape(rawURL)
	if err != nil {
		return "", err
	}
	return decoded, nil
}

func decodeJSONBody(reader io.Reader, dst any) error {
	data, err := io.ReadAll(reader)
	if err != nil {
		return err
	}
	if len(bytes.TrimSpace(data)) == 0 {
		return errors.New("empty request body")
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	return decoder.Decode(dst)
}

func copyRouteMap(source map[string]RouteCall) map[string]RouteCall {
	result := make(map[string]RouteCall, len(source))
	for key, handler := range source {
		result[key] = handler
	}
	return result
}

func writeMethodNotAllowed(w http.ResponseWriter) {
	writeError(w, NewActionFailed(http.StatusMethodNotAllowed, http.StatusText(http.StatusMethodNotAllowed), nil))
}

func writeError(w http.ResponseWriter, err error) {
	status := statusFromError(err)
	if err == nil {
		w.WriteHeader(status)
		return
	}
	http.Error(w, err.Error(), status)
}

func writeJSON(w http.ResponseWriter, status int, payload any) {
	data, err := json.Marshal(payload)
	if err != nil {
		writeError(w, err)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_, _ = w.Write(data)
}

func writeServerResponse(w http.ResponseWriter, response *Response) {
	if response == nil {
		w.WriteHeader(http.StatusOK)
		return
	}
	for key, values := range response.Header {
		for _, value := range values {
			w.Header().Add(key, value)
		}
	}
	w.WriteHeader(response.statusCodeOrDefault())
	if len(response.Body) > 0 {
		_, _ = w.Write(response.Body)
	}
}

func writeStreamResponse(w http.ResponseWriter, response *Response, chunkSize int) {
	if response == nil {
		w.WriteHeader(http.StatusOK)
		return
	}
	for key, values := range response.Header {
		for _, value := range values {
			w.Header().Add(key, value)
		}
	}
	w.WriteHeader(response.statusCodeOrDefault())

	if chunkSize <= 0 {
		chunkSize = defaultStreamChunkSize
	}
	for i := 0; i < len(response.Body); i += chunkSize {
		end := i + chunkSize
		if end > len(response.Body) {
			end = len(response.Body)
		}
		_, _ = w.Write(response.Body[i:end])
	}
}

func extractPlatformAndSelfID(header http.Header) (string, string, error) {
	platform := header.Get("X-Platform")
	if platform == "" {
		platform = header.Get("Satori-Platform")
	}
	if platform == "" {
		return "", "", Unauthorized("Missing header X-Platform or Satori-Platform")
	}

	selfID := header.Get("X-Self-ID")
	if selfID == "" {
		selfID = header.Get("Satori-User-ID")
	}
	if selfID == "" {
		return "", "", Unauthorized("Missing header X-Self-ID or Satori-User-ID")
	}
	return platform, selfID, nil
}

func randomToken(size int) (string, error) {
	if size <= 0 {
		size = 16
	}
	buffer := make([]byte, size)
	if _, err := rand.Read(buffer); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(buffer), nil
}

func toInt64(value any) (int64, bool) {
	switch typed := value.(type) {
	case int:
		return int64(typed), true
	case int8:
		return int64(typed), true
	case int16:
		return int64(typed), true
	case int32:
		return int64(typed), true
	case int64:
		return typed, true
	case uint:
		return int64(typed), true
	case uint8:
		return int64(typed), true
	case uint16:
		return int64(typed), true
	case uint32:
		return int64(typed), true
	case uint64:
		return int64(typed), true
	case float64:
		return int64(typed), true
	case float32:
		return int64(typed), true
	case json.Number:
		result, err := typed.Int64()
		if err == nil {
			return result, true
		}
		floatResult, err := typed.Float64()
		if err == nil {
			return int64(floatResult), true
		}
	case string:
		result, err := strconv.ParseInt(typed, 10, 64)
		if err == nil {
			return result, true
		}
	}
	return 0, false
}

func asString(value any) string {
	switch typed := value.(type) {
	case string:
		return typed
	case json.Number:
		return typed.String()
	default:
		if value == nil {
			return ""
		}
		return fmt.Sprint(value)
	}
}

func isLoginEventType(typ event.EventType) bool {
	return typ == event.EventTypeLoginAdded ||
		typ == event.EventTypeLoginRemoved ||
		typ == event.EventTypeLoginUpdated
}

func methodFilterHandler(next http.Handler, methods map[string]struct{}) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		if _, ok := methods[strings.ToUpper(request.Method)]; !ok {
			writeMethodNotAllowed(w)
			return
		}
		next.ServeHTTP(w, request)
	})
}

func toUpperMethods(methods []string) map[string]struct{} {
	if len(methods) == 0 {
		return nil
	}
	result := make(map[string]struct{}, len(methods))
	for _, method := range methods {
		method = strings.ToUpper(strings.TrimSpace(method))
		if method == "" {
			continue
		}
		result[method] = struct{}{}
	}
	return result
}
