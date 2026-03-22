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
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/gorilla/websocket"
	"github.com/satori-protocol-go/satori-go/pkg/satori/model/event"
	"github.com/satori-protocol-go/satori-go/pkg/satori/model/login"
	"github.com/satori-protocol-go/satori-go/pkg/satori/model/meta"
	"github.com/satori-protocol-go/satori-go/pkg/satori/model/operation"
	"golang.org/x/sync/errgroup"
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
	defaultCleanupTimeout  = 10 * time.Second
	defaultWebhookTimeout  = 300 * time.Second
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
	BaseHandler     http.Handler
	ReplaceRouter   chi.Router
	Webhooks        []WebhookEndpoint
	StreamThreshold int
	StreamChunkSize int
	EventCacheSize  int
	HTTPClient      *http.Client
	Logger          Logger
}

type staticResourceMount struct {
	targetPath string
	kind       staticMountKind
	html       bool
}

type Server struct {
	RouterMixin

	Host    string
	Port    int
	Path    string
	Version string
	Token   string

	streamThreshold int
	streamChunkSize int

	mu          sync.RWMutex
	routers     []Router
	adapters    []Adapter
	providers   []Provider
	rootRoutes  []rootRoute
	resources   map[string]staticResourceMount
	webhooks    []WebhookEndpoint
	connections map[*websocketConnection]struct{}

	sequence   int64
	eventCache eventDeque

	tempDir string

	httpClient    *http.Client
	httpServer    *http.Server
	listener      net.Listener
	logger        Logger
	baseHandler   http.Handler
	replaceRouter chi.Router

	runMu     sync.Mutex
	running   bool
	runCancel context.CancelFunc
	runDone   chan error

	registeredRouteTargets map[uintptr]struct{}
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
		return nil, errors.New("token is required when server host is public")
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
	logger := cfg.Logger
	if logger == nil {
		logger = NewStdLogger()
	}

	routerMixin := RouterMixin{}

	server := &Server{
		RouterMixin:            routerMixin,
		Host:                   host,
		Port:                   port,
		Path:                   path,
		Version:                version,
		Token:                  cfg.Token,
		streamThreshold:        streamThreshold,
		streamChunkSize:        streamChunkSize,
		resources:              map[string]staticResourceMount{},
		connections:            map[*websocketConnection]struct{}{},
		webhooks:               append([]WebhookEndpoint(nil), cfg.Webhooks...),
		eventCache:             newEventDeque(eventCacheSize),
		tempDir:                tempDir,
		httpClient:             httpClient,
		logger:                 logger,
		baseHandler:            cfg.BaseHandler,
		replaceRouter:          cfg.ReplaceRouter,
		registeredRouteTargets: map[uintptr]struct{}{},
	}

	return server, nil
}

func (s *Server) URLBase() string {
	return fmt.Sprintf("http://%s:%d%s/%s", s.Host, s.Port, s.Path, s.Version)
}

func (s *Server) RegisterLogger(logger Logger) {
	if logger == nil {
		logger = NopLogger{}
	}
	s.mu.Lock()
	s.logger = logger
	s.mu.Unlock()
}

func (s *Server) ReplaceRouter(router chi.Router) {
	s.mu.Lock()
	s.replaceRouter = router
	s.registeredRouteTargets = map[uintptr]struct{}{}
	s.mu.Unlock()
}

func (s *Server) Apply(item any) error {
	switch typed := item.(type) {
	case Adapter:
		typed.EnsureServer(s)
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

func (s *Server) Mount(routePath string, filePath string) error {
	return s.MountFile(routePath, filePath)
}

func (s *Server) MountFile(routePath string, filePath string) error {
	routePath = strings.TrimSpace(routePath)
	filePath = strings.TrimSpace(filePath)
	if routePath == "" || filePath == "" {
		return errors.New("routePath and filePath cannot be empty")
	}
	if !strings.HasPrefix(routePath, "/") {
		routePath = "/" + routePath
	}
	s.mu.Lock()
	s.resources[routePath] = staticResourceMount{
		targetPath: filePath,
		kind:       staticMountKindFile,
	}
	s.mu.Unlock()
	return nil
}

func (s *Server) MountDir(routePath string, directoryPath string, html bool) error {
	routePath = strings.TrimSpace(routePath)
	directoryPath = strings.TrimSpace(directoryPath)
	if routePath == "" || directoryPath == "" {
		return errors.New("routePath and directoryPath cannot be empty")
	}
	if !strings.HasPrefix(routePath, "/") {
		routePath = "/" + routePath
	}
	s.mu.Lock()
	s.resources[routePath] = staticResourceMount{
		targetPath: directoryPath,
		kind:       staticMountKindDirectory,
		html:       html,
	}
	s.mu.Unlock()
	return nil
}

func (s *Server) Handler() (http.Handler, error) {
	s.mu.RLock()
	replaceRouter := s.replaceRouter
	baseHandler := s.baseHandler
	s.mu.RUnlock()

	if replaceRouter != nil {
		// ReplaceRouter mode mounts Satori protocol routes into the caller-provided chi router.
		// Route conflict behavior is governed by chi's matcher precedence:
		// parent-level exact routes can override grouped Route(base, ...) handlers.
		// Consumers should register conflicting routes intentionally based on desired priority.
		if err := s.RegisterRoutes(replaceRouter); err != nil {
			return nil, err
		}
		return replaceRouter, nil
	}

	router := chi.NewRouter()
	if err := s.RegisterRoutes(router); err != nil {
		return nil, err
	}
	if baseHandler != nil {
		router.NotFound(baseHandler.ServeHTTP)
		router.MethodNotAllowed(baseHandler.ServeHTTP)
	}
	return router, nil
}

func (s *Server) RegisterRoutes(router chi.Router) error {
	if router == nil {
		return errors.New("router cannot be nil")
	}

	targetKey := routeTargetKey(router)
	if targetKey != 0 {
		s.mu.RLock()
		_, exists := s.registeredRouteTargets[targetKey]
		s.mu.RUnlock()
		if exists {
			return nil
		}
	}

	s.ensureDefaultUploadRoute()

	s.mountRootRoutes(router)
	s.mountAdapterRootRoutes(router)
	s.mountProtocolRoutes(router)
	if err := s.mountResources(router); err != nil {
		return err
	}

	if targetKey != 0 {
		s.mu.Lock()
		s.registeredRouteTargets[targetKey] = struct{}{}
		s.mu.Unlock()
	}
	return nil
}

func (s *Server) RouteHTTP(path string, methods []string, handler http.Handler) error {
	if len(methods) == 0 {
		methods = []string{http.MethodGet}
	}
	return s.Methods(path, handler, methods...)
}

func (s *Server) RouteWebSocket(path string, handler http.Handler) error {
	return s.RouteHTTP(path, []string{http.MethodGet}, handler)
}

func (s *Server) mountProtocolRoutes(router chi.Router) {
	base := s.apiBasePath()
	router.Route(base, func(r chi.Router) {
		r.Get("/events", s.websocketServerHandler)
		r.Post("/meta", s.metaGetHandler)
		r.Post("/meta/webhook.create", s.webhookCreateHandler)
		r.Post("/meta/webhook.delete", s.webhookDeleteHandler)
		for _, method := range [...]string{
			http.MethodGet,
			http.MethodPost,
			http.MethodPut,
			http.MethodDelete,
		} {
			r.MethodFunc(method, "/proxy/*", s.proxyURLHandler)
			r.MethodFunc(method, "/*", s.httpServerHandler)
		}
	})
}

func (s *Server) Run(ctx context.Context) error {
	if ctx == nil {
		ctx = context.Background()
	}

	runCtx, cancel := context.WithCancel(ctx)
	done, err := s.beginRun(cancel)
	if err != nil {
		cancel()
		return err
	}
	defer cancel()

	var runErr error
	defer s.finishRun(done, runErr)

	if err := s.runPreparing(runCtx); err != nil {
		runErr = err
		cleanupCtx, cancelCleanup := context.WithTimeout(context.Background(), defaultCleanupTimeout)
		cleanupErr := s.runCleanup(cleanupCtx)
		cancelCleanup()
		if cleanupErr != nil {
			runErr = errors.Join(runErr, cleanupErr)
		}
		return runErr
	}

	blockErr := s.runBlocking(runCtx)
	cleanupCtx, cancelCleanup := context.WithTimeout(context.Background(), defaultCleanupTimeout)
	cleanupErr := s.runCleanup(cleanupCtx)
	cancelCleanup()
	runErr = composeRunError(blockErr, cleanupErr, ctx.Err())
	return runErr
}

func (s *Server) Shutdown(ctx context.Context) error {
	if ctx == nil {
		ctx = context.Background()
	}

	s.runMu.Lock()
	cancel := s.runCancel
	done := s.runDone
	running := s.running
	s.runMu.Unlock()

	if running {
		if cancel != nil {
			cancel()
		}
		if done == nil {
			return nil
		}
		select {
		case err, ok := <-done:
			if !ok {
				return nil
			}
			return err
		case <-ctx.Done():
			return ctx.Err()
		}
	}

	if !s.hasRuntimeResources() {
		return nil
	}
	return s.runCleanup(ctx)
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
			s.log(context.Background(), LogLevelWarn, "websocket broadcast failed",
				Field{Key: "connection_id", Value: connection.ID()},
				Field{Key: "remote_addr", Value: connection.RemoteAddr()},
				Field{Key: "error", Value: err},
			)
			_ = connection.Close()
			s.removeConnection(connection)
		}
	}

	for _, webhook := range webhooks {
		if err := s.sendWebhook(webhook, operation.OpcodeEvent, evt); err != nil {
			s.log(context.Background(), LogLevelError, "webhook event delivery failed",
				Field{Key: "url", Value: webhook.URL},
				Field{Key: "opcode", Value: operation.OpcodeEvent},
				Field{Key: "error", Value: err},
			)
		}
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

	proxyURLs := s.getProxyURLs()
	if err := s.sendWebhook(hook, operation.OpcodeMeta, map[string]any{"proxy_urls": proxyURLs}); err != nil {
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
	connection := newWebsocketConnection(
		conn,
		request.RemoteAddr,
		func(level LogLevel, message string, fields ...Field) {
			s.log(request.Context(), level, message, fields...)
		},
	)
	defer connection.Close()
	acceptFields := []Field{
		{Key: "connection_id", Value: connection.ID()},
		{Key: "remote_addr", Value: connection.RemoteAddr()},
	}
	if subprotocol := conn.Subprotocol(); subprotocol != "" {
		acceptFields = append(acceptFields, Field{Key: "subprotocol", Value: subprotocol})
	}
	s.log(request.Context(), LogLevelInfo, "websocket accepted", acceptFields...)

	token, sequence, err := readIdentify(connection)
	if err != nil {
		s.log(request.Context(), LogLevelWarn, "websocket identify failed",
			Field{Key: "connection_id", Value: connection.ID()},
			Field{Key: "remote_addr", Value: connection.RemoteAddr()},
			Field{Key: "error", Value: err},
		)
		_ = connection.CloseWith(3000, "Unauthorized")
		return
	}
	if token != s.Token {
		s.log(request.Context(), LogLevelWarn, "websocket unauthorized token",
			Field{Key: "connection_id", Value: connection.ID()},
			Field{Key: "remote_addr", Value: connection.RemoteAddr()},
		)
		_ = connection.CloseWith(3000, "Unauthorized")
		return
	}

	logins, proxyUrls, err := s.collectMeta(request.Context())
	if err != nil {
		s.log(request.Context(), LogLevelError, "websocket prepare ready failed",
			Field{Key: "connection_id", Value: connection.ID()},
			Field{Key: "remote_addr", Value: connection.RemoteAddr()},
			Field{Key: "error", Value: err},
		)
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
		s.log(request.Context(), LogLevelWarn, "websocket send ready failed",
			Field{Key: "connection_id", Value: connection.ID()},
			Field{Key: "remote_addr", Value: connection.RemoteAddr()},
			Field{Key: "error", Value: err},
		)
		return
	}
	s.log(request.Context(), LogLevelDebug, "websocket ready sent",
		Field{Key: "connection_id", Value: connection.ID()},
		Field{Key: "remote_addr", Value: connection.RemoteAddr()},
	)

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

	go connection.Heartbeat(defaultHeartbeat)
	connection.WaitClosed()
	closeReason, closeErr := connection.CloseInfo()
	lastHeartbeatAt, lastHeartbeatLatency := connection.LastHeartbeat()
	fields := []Field{
		{Key: "connection_id", Value: connection.ID()},
		{Key: "remote_addr", Value: connection.RemoteAddr()},
		{Key: "reason", Value: closeReason},
	}
	if closeErr != nil {
		fields = append(fields, Field{Key: "error", Value: closeErr})
	}
	if !lastHeartbeatAt.IsZero() {
		fields = append(fields,
			Field{Key: "last_heartbeat_at", Value: lastHeartbeatAt.Format(time.RFC3339Nano)},
			Field{Key: "last_heartbeat_latency_ms", Value: lastHeartbeatLatency.Milliseconds()},
		)
	}
	s.log(request.Context(), LogLevelInfo, "websocket closed", fields...)
}

func (s *Server) httpServerHandler(w http.ResponseWriter, request *http.Request) {
	s.ensureDefaultUploadRoute()

	action := s.extractAction(request)
	s.mu.RLock()
	hasAdapters := len(s.adapters) > 0
	hasServerRoutes := len(s.routes) > 0
	s.mu.RUnlock()
	if !hasAdapters && !hasServerRoutes {
		writeError(w, NotFound(action))
		return
	}
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
	rawURL := strings.TrimPrefix(chi.URLParam(request, "*"), "/")
	if rawURL == "" {
		base := s.apiBasePath() + "/proxy/"
		rawURL = strings.TrimPrefix(request.URL.Path, base)
	}
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
		writeError(w, NewActionError(http.StatusInternalServerError, "empty proxy response", nil))
		return
	}

	if resp.Stream != nil {
		writeServerResponse(w, resp, s.streamChunkSize)
		return
	}
	if len(resp.Body) > s.streamThreshold {
		writeServerResponse(w, resp, s.streamChunkSize)
		return
	}
	writeServerResponse(w, resp, 0)
}

func (s *Server) executeRoute(
	w http.ResponseWriter,
	request *http.Request,
	action string,
	platform string,
	selfID string,
	handler RouteCall[any, any],
) {
	params, err := parseParams(action, request)
	if err != nil {
		writeError(w, err)
		return
	}

	result, callErr := handler(&Request[any]{
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
		writeServerResponse(w, typed, 0)
	case Response:
		writeServerResponse(w, &typed, 0)
	default:
		writeJSON(w, http.StatusOK, typed)
	}
}

func (s *Server) findRouteHandler(action string, platform string, selfID string) (RouteCall[any, any], bool) {
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
	tmpPath := ""
	hasTmpPrefix := strings.HasPrefix(path, "_tmp")
	if hasTmpPrefix && len(path) > 5 {
		tmpPath = path[5:]
	}

	if hasTmpPrefix {
		resp, err := s.fetchTempFile(tmpPath)
		if err == nil && resp != nil {
			return resp, nil
		}
		if err != nil && statusFromError(err) != http.StatusNotFound {
			return nil, err
		}
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

	if hasTmpPrefix {
		return nil, NotFound(fmt.Sprintf("file not found: %s", tmpPath))
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
	file, err := os.Open(filePath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, NotFound(fmt.Sprintf("file not found: %s", cleanName))
		}
		return nil, err
	}
	info, statErr := file.Stat()
	if statErr != nil {
		_ = file.Close()
		return nil, statErr
	}

	contentType := mime.TypeByExtension(filepath.Ext(filePath))
	if contentType == "" {
		contentType = "application/octet-stream"
	}
	resp := NewStreamResponse(http.StatusOK, file)
	resp.Header.Set("Content-Type", contentType)
	resp.ContentLength = info.Size()
	resp.Header.Set("Content-Length", strconv.FormatInt(info.Size(), 10))
	return resp, nil
}

func (s *Server) defaultUploadCreateHandler(request *Request[UploadCreateParam]) (map[string]string, error) {
	if request == nil || request.Params == nil {
		return nil, BadRequest("invalid form data")
	}
	form := request.Params

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

func (s *Server) collectLogins(ctx context.Context) ([]*login.Login, error) {
	logins := make([]*login.Login, 0)

	for _, provider := range s.snapshotProviders() {
		items, err := provider.GetLogins(ctx)
		if err != nil {
			return nil, err
		}
		logins = append(logins, items...)
	}
	return logins, nil
}

func (s *Server) getProxyURLs() []string {
	proxyURLs := make([]string, 0)
	for _, provider := range s.snapshotProviders() {
		proxyURLs = append(proxyURLs, provider.ProxyUrls()...)
	}
	return proxyURLs
}

func (s *Server) collectMeta(ctx context.Context) ([]*login.Login, []string, error) {
	logins, err := s.collectLogins(ctx)
	if err != nil {
		return nil, nil, err
	}
	return logins, s.getProxyURLs(), nil
}

func (s *Server) broadcastMetaToWebhooks(ctx context.Context) error {
	proxyURLs := s.getProxyURLs()
	s.mu.RLock()
	webhooks := append([]WebhookEndpoint(nil), s.webhooks...)
	s.mu.RUnlock()

	body := map[string]any{"proxy_urls": proxyURLs}
	for _, webhook := range webhooks {
		if err := s.sendWebhook(webhook, operation.OpcodeMeta, body); err != nil {
			s.log(ctx, LogLevelError, "webhook meta delivery failed",
				Field{Key: "url", Value: webhook.URL},
				Field{Key: "opcode", Value: operation.OpcodeMeta},
				Field{Key: "error", Value: err},
			)
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

	timeout := webhook.Timeout
	if timeout <= 0 {
		timeout = defaultWebhookTimeout
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
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
	if wildcard := strings.TrimPrefix(chi.URLParam(request, "*"), "/"); wildcard != "" {
		return wildcard
	}
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

func (s *Server) snapshotAdapters() []Adapter {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return append([]Adapter(nil), s.adapters...)
}

func (s *Server) log(ctx context.Context, level LogLevel, message string, fields ...Field) {
	s.mu.RLock()
	logger := s.logger
	s.mu.RUnlock()
	if logger == nil {
		return
	}
	logger.Log(ctx, level, message, fields...)
}

func (s *Server) beginRun(cancel context.CancelFunc) (chan error, error) {
	s.runMu.Lock()
	defer s.runMu.Unlock()

	if s.running {
		return nil, errors.New("server is already running")
	}
	done := make(chan error, 1)
	s.running = true
	s.runCancel = cancel
	s.runDone = done
	return done, nil
}

func (s *Server) finishRun(done chan error, runErr error) {
	s.runMu.Lock()
	if s.runDone == done {
		s.running = false
		s.runCancel = nil
		s.runDone = nil
	}
	s.runMu.Unlock()

	if done != nil {
		done <- runErr
		close(done)
	}
}

func (s *Server) hasRuntimeResources() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.httpServer != nil || s.listener != nil || len(s.connections) > 0 || s.tempDir != ""
}

func composeRunError(blockErr error, cleanupErr error, parentErr error) error {
	if blockErr != nil {
		switch {
		case errors.Is(blockErr, context.Canceled), errors.Is(blockErr, context.DeadlineExceeded):
			if parentErr != nil {
				blockErr = nil
			}
		case errors.Is(blockErr, http.ErrServerClosed):
			blockErr = nil
		}
	}
	if cleanupErr != nil {
		switch {
		case errors.Is(cleanupErr, context.Canceled), errors.Is(cleanupErr, context.DeadlineExceeded):
			if parentErr != nil {
				cleanupErr = nil
			}
		case errors.Is(cleanupErr, http.ErrServerClosed):
			cleanupErr = nil
		}
	}
	if blockErr == nil && cleanupErr == nil {
		return nil
	}
	if blockErr != nil && cleanupErr != nil {
		return errors.Join(blockErr, cleanupErr)
	}
	if blockErr != nil {
		return blockErr
	}
	return cleanupErr
}

func (s *Server) ensureTempDir() error {
	s.mu.RLock()
	tempDir := s.tempDir
	s.mu.RUnlock()
	if tempDir != "" {
		return nil
	}

	created, err := os.MkdirTemp("", "satori-server-*")
	if err != nil {
		return err
	}

	s.mu.Lock()
	if s.tempDir == "" {
		s.tempDir = created
		created = ""
	}
	s.mu.Unlock()
	if created != "" {
		_ = os.RemoveAll(created)
	}
	return nil
}

func (s *Server) runPreparing(ctx context.Context) error {
	if err := s.ensureTempDir(); err != nil {
		return err
	}
	s.ensureDefaultUploadRoute()
	handler, err := s.Handler()
	if err != nil {
		s.log(ctx, LogLevelError, "build server handler failed", Field{Key: "error", Value: err})
		return err
	}

	addr := net.JoinHostPort(s.Host, strconv.Itoa(s.Port))
	listener, err := net.Listen("tcp", addr)
	if err != nil {
		s.log(ctx, LogLevelError, "listen failed", Field{Key: "address", Value: addr}, Field{Key: "error", Value: err})
		return err
	}

	httpServer := &http.Server{
		Addr:    addr,
		Handler: handler,
	}
	s.mu.Lock()
	s.httpServer = httpServer
	s.listener = listener
	s.mu.Unlock()

	for _, adapter := range s.snapshotAdapters() {
		preparable, ok := any(adapter).(Preparable)
		if !ok {
			continue
		}
		if err := preparable.Prepare(ctx); err != nil {
			return err
		}
	}
	return nil
}

func (s *Server) runBlocking(ctx context.Context) error {
	s.mu.RLock()
	httpServer := s.httpServer
	listener := s.listener
	s.mu.RUnlock()
	if httpServer == nil {
		return errors.New("http server is not prepared")
	}
	if listener == nil {
		return errors.New("http listener is not prepared")
	}

	taskCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	group, groupCtx := errgroup.WithContext(taskCtx)
	firstDone := make(chan struct{})
	var firstOnce sync.Once
	var firstErr error
	recordFirst := func(err error) {
		firstOnce.Do(func() {
			firstErr = err
			close(firstDone)
		})
	}

	group.Go(func() error {
		err := s.runHTTPServerTask(groupCtx, httpServer, listener)
		recordFirst(err)
		return err
	})

	for _, provider := range s.snapshotProviders() {
		publisher, ok := provider.(EventPublisher)
		if !ok {
			continue
		}
		stream := publisher.Publisher(groupCtx)
		group.Go(func() error {
			err := s.runPublisherTask(groupCtx, stream)
			recordFirst(err)
			return err
		})
	}

	for _, adapter := range s.snapshotAdapters() {
		blockable, ok := any(adapter).(Blockable)
		if !ok {
			continue
		}
		group.Go(func() error {
			err := blockable.Block(groupCtx)
			recordFirst(err)
			return err
		})
	}

	select {
	case <-firstDone:
		cancel()
		_ = group.Wait()
		return firstErr
	default:
	}

	if err := s.broadcastMetaToWebhooks(ctx); err != nil {
		recordFirst(err)
		cancel()
		_ = group.Wait()
		return firstErr
	}

	select {
	case <-firstDone:
	case <-ctx.Done():
		recordFirst(ctx.Err())
	}

	cancel()
	_ = group.Wait()
	return firstErr
}

func (s *Server) runHTTPServerTask(ctx context.Context, httpServer *http.Server, listener net.Listener) error {
	if httpServer == nil {
		return errors.New("http server is nil")
	}
	if listener == nil {
		return errors.New("http listener is nil")
	}

	errCh := make(chan error, 1)
	go func() {
		errCh <- httpServer.Serve(listener)
	}()

	select {
	case err := <-errCh:
		if errors.Is(err, http.ErrServerClosed) || errors.Is(err, net.ErrClosed) {
			return nil
		}
		return err
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = httpServer.Shutdown(shutdownCtx)
		err := <-errCh
		if errors.Is(err, http.ErrServerClosed) || errors.Is(err, net.ErrClosed) {
			return nil
		}
		if err == nil {
			return nil
		}
		return err
	}
}

func (s *Server) runPublisherTask(ctx context.Context, stream <-chan *event.Event) error {
	if stream == nil {
		<-ctx.Done()
		return ctx.Err()
	}
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case evt, ok := <-stream:
			if !ok {
				return nil
			}
			if err := s.Post(evt); err != nil {
				return err
			}
		}
	}
}

func (s *Server) runCleanup(ctx context.Context) error {
	if ctx == nil {
		ctx = context.Background()
	}

	s.mu.Lock()
	httpServer := s.httpServer
	s.httpServer = nil
	listener := s.listener
	s.listener = nil

	connections := make([]*websocketConnection, 0, len(s.connections))
	for connection := range s.connections {
		connections = append(connections, connection)
	}
	s.connections = map[*websocketConnection]struct{}{}

	tempDir := s.tempDir
	s.tempDir = ""
	s.mu.Unlock()

	var cleanupErr error

	for _, connection := range connections {
		if err := connection.Close(); err != nil {
			cleanupErr = errors.Join(cleanupErr, err)
		}
	}

	if httpServer != nil {
		if err := httpServer.Shutdown(ctx); err != nil && !errors.Is(err, http.ErrServerClosed) {
			cleanupErr = errors.Join(cleanupErr, err)
		}
	}
	if listener != nil {
		if err := listener.Close(); err != nil && !errors.Is(err, net.ErrClosed) {
			cleanupErr = errors.Join(cleanupErr, err)
		}
	}

	for _, adapter := range s.snapshotAdapters() {
		cleanable, ok := any(adapter).(Cleanable)
		if !ok {
			continue
		}
		if err := cleanable.Cleanup(ctx); err != nil {
			cleanupErr = errors.Join(cleanupErr, err)
		}
	}

	if tempDir != "" {
		if err := os.RemoveAll(tempDir); err != nil {
			cleanupErr = errors.Join(cleanupErr, err)
		}
	}

	return cleanupErr
}

func (s *Server) ensureDefaultUploadRoute() {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.routes == nil {
		s.routes = map[string]RouteCall[any, any]{}
	}
	if _, exists := s.routes[string(ApiUploadCreate)]; exists {
		return
	}
	s.routes[string(ApiUploadCreate)] = Wrapper(s.defaultUploadCreateHandler)
}

func (s *Server) mountResources(router chi.Router) error {
	s.mu.RLock()
	resources := map[string]staticResourceMount{}
	for routePath, resource := range s.resources {
		resources[routePath] = resource
	}
	s.mu.RUnlock()

	for routePath, resource := range resources {
		var (
			mount *staticFilesMount
			err   error
		)
		switch resource.kind {
		case staticMountKindDirectory:
			mount, err = newStaticFilesMountFromDirectory(routePath, resource.targetPath, resource.html)
		case staticMountKindFile:
			mount, err = newStaticFilesMountFromFile(routePath, resource.targetPath)
		default:
			err = fmt.Errorf("unsupported static mount kind %q", resource.kind)
		}
		if err != nil {
			return fmt.Errorf("invalid static mount %q => %q: %w", routePath, resource.targetPath, err)
		}

		router.Handle(mount.Pattern(), mount)
		router.Handle(mount.RoutePath(), mount)
	}
	return nil
}

func (s *Server) mountRootRoutes(router chi.Router) {
	s.mu.RLock()
	rootRoutes := append([]rootRoute(nil), s.rootRoutes...)
	s.mu.RUnlock()

	for _, route := range rootRoutes {
		if route.method == "" {
			router.Handle(route.pattern, route.handler)
			continue
		}
		router.Method(route.method, route.pattern, route.handler)
	}
}

func (s *Server) mountAdapterRootRoutes(router chi.Router) {
	s.mu.RLock()
	adapters := append([]Adapter(nil), s.adapters...)
	s.mu.RUnlock()

	for _, adapter := range adapters {
		registrar, ok := any(adapter).(RootRouteRegistrar)
		if !ok || registrar == nil {
			continue
		}
		registrar.RegisterRootRoutes(router)
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
			if len(values) == 0 {
				continue
			}
			params[key] = values[len(values)-1]
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

func copyRouteMap(source map[string]RouteCall[any, any]) map[string]RouteCall[any, any] {
	result := make(map[string]RouteCall[any, any], len(source))
	for key, handler := range source {
		result[key] = handler
	}
	return result
}

func writeMethodNotAllowed(w http.ResponseWriter) {
	writeError(w, NewActionError(http.StatusMethodNotAllowed, http.StatusText(http.StatusMethodNotAllowed), nil))
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

func writeServerResponse(w http.ResponseWriter, response *Response, chunkSize int) {
	if response == nil {
		w.WriteHeader(http.StatusOK)
		return
	}
	for key, values := range response.Header {
		for _, value := range values {
			w.Header().Add(key, value)
		}
	}
	if response.ContentLength >= 0 && w.Header().Get("Content-Length") == "" {
		w.Header().Set("Content-Length", strconv.FormatInt(response.ContentLength, 10))
	}
	w.WriteHeader(response.statusCodeOrDefault())

	if response.Stream != nil {
		defer response.closeStream()
		if chunkSize <= 0 {
			chunkSize = defaultStreamChunkSize
		}
		buffer := make([]byte, chunkSize)
		_, _ = io.CopyBuffer(w, response.Stream, buffer)
		return
	}

	if len(response.Body) == 0 {
		return
	}

	if chunkSize <= 0 {
		_, _ = w.Write(response.Body)
		return
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

func routeTargetKey(router chi.Router) uintptr {
	if router == nil {
		return 0
	}
	value := reflect.ValueOf(router)
	switch value.Kind() {
	case reflect.Pointer, reflect.Map, reflect.Slice, reflect.Func, reflect.Chan, reflect.UnsafePointer:
		return value.Pointer()
	default:
		return 0
	}
}
