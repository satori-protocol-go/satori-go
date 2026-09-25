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
	"github.com/satori-protocol-go/satori-go/pkg/satori/logging"
	"github.com/satori-protocol-go/satori-go/pkg/satori/model/event"
	"github.com/satori-protocol-go/satori-go/pkg/satori/model/login"
	"github.com/satori-protocol-go/satori-go/pkg/satori/model/meta"
	"github.com/satori-protocol-go/satori-go/pkg/satori/model/operation"
	"github.com/satori-protocol-go/satori-go/pkg/satori/protocol"
	"golang.org/x/sync/errgroup"
)

const (
	defaultHost            = "127.0.0.1"
	defaultPort            = protocol.DefaultAPIPort
	defaultVersion         = protocol.DefaultVersion
	defaultEventCacheSize  = 100
	defaultStreamThreshold = 16 * 1024 * 1024
	defaultStreamChunkSize = 64 * 1024
	defaultHeartbeat       = 12 * time.Second
	defaultIdentifyTimeout = 10 * time.Second
	defaultCleanupTimeout  = 10 * time.Second
	defaultWebhookTimeout  = protocol.DefaultRequestTimeout
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
	ResponseHeaders http.Header
	BaseHandler     http.Handler
	ReplaceRouter   chi.Router
	Webhooks        []WebhookEndpoint
	StreamThreshold int
	StreamChunkSize int
	EventCacheSize  int
	MaxRequestBytes int64 // Local limit for standard RPC/upload bodies; default 32 MiB.
	HTTPClient      *http.Client
	Logger          Logger
}

type staticResourceMount struct {
	targetPath string
	kind       staticMountKind
	html       bool
}

type temporaryFile struct {
	platform, selfID, contentType string
	expires                       time.Time
	timer                         *time.Timer
}

type providerLoginKey struct {
	source int
	sn     int64
}
type loginBinding struct {
	sn   int64
	info *login.Login
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

	// Lock order: publishMu, then mu. Provider calls and network I/O never hold mu.
	publishMu   sync.Mutex
	mu          sync.RWMutex
	routers     []Router
	adapters    []Adapter
	providers   []Provider
	rootRoutes  []rootRoute
	resources   map[string]staticResourceMount
	webhooks    []WebhookEndpoint
	connections map[*websocketConnection]struct{}

	sequence      int64
	eventCache    eventDeque
	loginSequence int64
	loginMappings map[providerLoginKey]*loginBinding

	tempDir         string
	tempFiles       map[string]*temporaryFile
	maxRequestBytes int64

	httpClient     *http.Client
	httpServer     *http.Server
	listener       net.Listener
	logger         Logger
	responseHeader http.Header
	baseHandler    http.Handler
	replaceRouter  chi.Router

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

	maxRequestBytes := cfg.MaxRequestBytes
	if maxRequestBytes <= 0 {
		maxRequestBytes = 32 << 20
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
		loginMappings:          map[providerLoginKey]*loginBinding{},
		tempDir:                tempDir,
		tempFiles:              map[string]*temporaryFile{},
		maxRequestBytes:        maxRequestBytes,
		httpClient:             httpClient,
		logger:                 logger,
		responseHeader:         cloneHTTPHeader(cfg.ResponseHeaders),
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

func (s *Server) SetResponseHeader(key string, value string) {
	key = strings.TrimSpace(key)
	if key == "" {
		return
	}
	s.mu.Lock()
	if s.responseHeader == nil {
		s.responseHeader = http.Header{}
	}
	s.responseHeader.Set(key, value)
	s.mu.Unlock()
}

func (s *Server) AddResponseHeader(key string, value string) {
	key = strings.TrimSpace(key)
	if key == "" {
		return
	}
	s.mu.Lock()
	if s.responseHeader == nil {
		s.responseHeader = http.Header{}
	}
	s.responseHeader.Add(key, value)
	s.mu.Unlock()
}

func (s *Server) SetResponseHeaders(header http.Header) {
	s.mu.Lock()
	s.responseHeader = cloneHTTPHeader(header)
	s.mu.Unlock()
}

func (s *Server) ResponseHeaders() http.Header {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return cloneHTTPHeader(s.responseHeader)
}

func (s *Server) applyResponseHeaders(w http.ResponseWriter) {
	if w == nil {
		return
	}
	headers := s.ResponseHeaders()
	for key, values := range headers {
		if len(values) == 0 {
			continue
		}
		w.Header().Del(key)
		for _, value := range values {
			w.Header().Add(key, value)
		}
	}
}

func (s *Server) wrapResponseHeaders(next http.Handler) http.Handler {
	if next == nil {
		return nil
	}
	return http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		s.applyResponseHeaders(w)
		next.ServeHTTP(w, request)
	})
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

	var router chi.Router
	if replaceRouter != nil {
		// ReplaceRouter mode mounts Satori protocol routes into the caller-provided chi router.
		// Route conflict behavior is governed by chi's matcher precedence:
		// parent-level exact routes can override grouped Route(base, ...) handlers.
		// Consumers should register conflicting routes intentionally based on desired priority.
		router = replaceRouter
	} else {
		router = chi.NewRouter()
	}

	if err := s.RegisterRoutes(router); err != nil {
		return nil, err
	}
	if baseHandler != nil {
		router.NotFound(baseHandler.ServeHTTP)
		router.MethodNotAllowed(baseHandler.ServeHTTP)
	}
	return s.wrapResponseHeaders(router), nil
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
		r.Post("/meta", s.authorized(s.metaGetHandler))
		r.Post("/meta/webhook.create", s.authorized(s.webhookCreateHandler))
		r.Post("/meta/webhook.delete", s.authorized(s.webhookDeleteHandler))
		for _, method := range [...]string{
			http.MethodGet,
			http.MethodPost,
			http.MethodPut,
			http.MethodPatch,
			http.MethodHead,
			http.MethodOptions,
			http.MethodDelete,
		} {
			r.MethodFunc(method, "/proxy/*", s.proxyURLHandler)
			r.MethodFunc(method, "/*", s.authorized(s.httpServerHandler))
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
	defer func() {
		s.finishRun(done, runErr)
		level := LogLevelInfo
		if runErr != nil {
			level = LogLevelError
		}
		description := "Satori server stopped."
		if runErr != nil {
			description = "Satori server stopped with an error: " + logging.ErrorText(runErr)
		}
		s.log(ctx, level, description)
	}()

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

	s.mu.RLock()
	address := net.JoinHostPort(s.Host, strconv.Itoa(s.Port))
	if s.listener != nil {
		address = s.listener.Addr().String()
	}
	s.mu.RUnlock()
	s.log(runCtx, LogLevelInfo, fmt.Sprintf("Satori server is listening on %s.", address))
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

// Post publishes an event from a uniquely identifiable registered login.
// Provider publishers are bound directly to their source by the running server.
func (s *Server) Post(evt *event.Event) error {
	if evt == nil {
		return nil
	}
	if evt.Login == nil {
		return errors.New("event login is required")
	}
	source, err := s.eventSource(evt.Login)
	if err != nil {
		return err
	}
	return s.postFrom(context.Background(), source, evt)
}

func (s *Server) postFrom(ctx context.Context, source int, evt *event.Event) error {
	s.publishMu.Lock()
	defer s.publishMu.Unlock()
	if evt == nil {
		return nil
	}
	if evt.Login == nil || evt.Login.Sn < 0 {
		return errors.New("invalid event login")
	}
	owned := *evt
	s.mu.Lock()
	binding := s.bindLoginLocked(source, evt.Login)
	if isLoginEventType(evt.Type) {
		binding.info = binding.info.Merge(evt.Login)
	}
	owned.Login = binding.info.Merge(evt.Login)
	owned.Login.Sn = binding.sn
	owned.Sn = s.sequence
	body, err := json.Marshal(&owned)
	if err != nil {
		s.mu.Unlock()
		return err
	}
	frozen := cachedEvent{sn: owned.Sn, kind: owned.Type, body: body}
	s.sequence++
	s.eventCache.Append(frozen)
	connections := make([]*websocketConnection, 0, len(s.connections))
	for connection := range s.connections {
		connections = append(connections, connection)
	}
	webhooks := append([]WebhookEndpoint(nil), s.webhooks...)
	s.mu.Unlock()

	payload := map[string]any{"op": operation.OpcodeEvent, "body": frozen.body}
	for _, connection := range connections {
		if !connection.Alive() {
			continue
		}
		if err := connection.Send(payload); err != nil {
			s.log(context.Background(), LogLevelWarn, fmt.Sprintf("Failed to send an event to WebSocket client %s: %s", connection.ID(), logging.ErrorText(err)))
			_ = connection.Close()
			s.removeConnection(connection)
		}
	}
	var deliveryErr error
	for _, webhook := range webhooks {
		if err := s.sendWebhook(ctx, webhook, operation.OpcodeEvent, frozen.body); err != nil {
			deliveryErr = errors.Join(deliveryErr, err)
		}
	}
	return deliveryErr
}

// bindLoginLocked assigns a runtime-local downstream number. Source keys remain
// available for late lifecycle events and are not persisted beyond this server.
func (s *Server) bindLoginLocked(source int, info *login.Login) *loginBinding {
	key := providerLoginKey{source: source, sn: info.Sn}
	if binding := s.loginMappings[key]; binding != nil {
		return binding
	}
	binding := &loginBinding{sn: s.loginSequence, info: info.Clone()}
	s.loginSequence++
	s.loginMappings[key] = binding
	return binding
}

func (s *Server) eventSource(info *login.Login) (int, error) {
	for attempt := 0; attempt < 2; attempt++ {
		s.mu.RLock()
		if len(s.providers) == 0 {
			s.mu.RUnlock()
			return 0, nil
		}
		source, count := 0, 0
		for key, binding := range s.loginMappings {
			if key.source == 0 {
				continue
			}
			match := key.sn == info.Sn
			if info.User != nil && info.Platform != "" {
				match = binding.info.User != nil && binding.info.Platform == info.Platform && binding.info.User.Id == info.User.Id
			}
			if match {
				source = key.source
				count++
			}
		}
		s.mu.RUnlock()
		if count == 1 {
			return source, nil
		}
		if count > 1 {
			return 0, errors.New("ambiguous event source; use the provider publisher")
		}
		if attempt == 0 {
			if _, err := s.collectLogins(context.Background()); err != nil {
				return 0, err
			}
		}
	}
	return 0, errors.New("event source login is not registered")
}

// GetLocalFile reads an owned upload using its complete internal URL.
func (s *Server) GetLocalFile(rawURL string) ([]byte, error) {
	match := internalURLPattern.FindStringSubmatch(rawURL)
	if len(match) != 4 || !strings.HasPrefix(match[3], "_tmp/") {
		return nil, BadRequest("expected an internal temporary resource")
	}
	if !s.hasLogin(match[1], match[2]) {
		return nil, NotFound("resource login not found")
	}
	response, err := s.fetchTempFile(match[1], match[2], strings.TrimPrefix(match[3], "_tmp/"))
	if err != nil {
		return nil, err
	}
	defer response.closeStream()
	return io.ReadAll(io.LimitReader(response.Stream, s.maxRequestBytes+1))
}

func (s *Server) hasLogin(platform, selfID string) bool {
	for _, provider := range s.snapshotProviders() {
		if provider.Ensure(platform, selfID) {
			return true
		}
	}
	return false
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
	if _, err := writeJSON(w, http.StatusOK, meta.Meta{Logins: logins, ProxyUrls: proxyUrls}); err != nil {
		s.log(request.Context(), LogLevelError, fmt.Sprintf("Failed to deliver Satori metadata to the HTTP client: %s", logging.ErrorText(err)))
	}
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
	if err := s.sendWebhook(request.Context(), hook, operation.OpcodeMeta, map[string]any{"proxy_urls": proxyURLs}); err != nil {
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
		func(level LogLevel, v ...any) {
			s.log(request.Context(), level, v...)
		},
	)
	defer connection.Close()
	acceptMessage := fmt.Sprintf("Accepted WebSocket client %s from %s.", connection.ID(), logging.SafeText(connection.RemoteAddr()))
	if subprotocol := conn.Subprotocol(); subprotocol != "" {
		acceptMessage += fmt.Sprintf(" Negotiated subprotocol %q.", subprotocol)
	}
	s.log(request.Context(), LogLevelInfo, acceptMessage)

	token, sequence, err := readIdentify(connection)
	if err != nil {
		description := fmt.Sprintf("Failed to read IDENTIFY from WebSocket client %s at %s: %s", connection.ID(), logging.SafeText(connection.RemoteAddr()), logging.ErrorText(err))
		if isTimeoutError(err) {
			description = fmt.Sprintf("WebSocket client %s at %s timed out during identification.", connection.ID(), logging.SafeText(connection.RemoteAddr()))
		}
		s.log(request.Context(), LogLevelWarn, description)
		_ = connection.CloseWith(3000, "Unauthorized")
		return
	}
	if s.Token != "" && token != s.Token {
		s.log(
			request.Context(),
			LogLevelWarn,
			fmt.Sprintf("Rejected WebSocket client %s at %s because its access token is invalid.", connection.ID(), logging.SafeText(connection.RemoteAddr())),
		)
		_ = connection.CloseWith(3000, "Unauthorized")
		return
	}

	if err := s.openEventStream(request.Context(), connection, sequence); err != nil {
		s.log(request.Context(), LogLevelWarn, fmt.Sprintf("Failed to initialize the event stream for WebSocket client %s: %s", connection.ID(), logging.ErrorText(err)))
		return
	}
	defer s.removeConnection(connection)

	connection.WaitClosed()
	closeReason, closeErr := connection.CloseInfo()
	lastHeartbeatAt, lastHeartbeatLatency := connection.LastHeartbeat()
	var closedBuilder strings.Builder
	closedBuilder.WriteString(fmt.Sprintf("WebSocket client %s at %s closed: %s.", connection.ID(), logging.SafeText(connection.RemoteAddr()), logging.SafeText(closeReason)))
	if closeErr != nil {
		closedBuilder.WriteString(fmt.Sprintf(" Connection error: %s.", logging.ErrorText(closeErr)))
	}
	if !lastHeartbeatAt.IsZero() {
		closedBuilder.WriteString(fmt.Sprintf(
			" Last heartbeat at %s; input wait was %d ms.",
			lastHeartbeatAt.Format(time.RFC3339Nano),
			lastHeartbeatLatency.Milliseconds(),
		))
	}
	s.log(request.Context(), LogLevelInfo, closedBuilder.String())
}

// openEventStream shares the publication order boundary. Provider snapshots run
// without the general state lock; events produced meanwhile wait for this handoff.
func (s *Server) openEventStream(ctx context.Context, connection *websocketConnection, sequence int64) error {
	s.publishMu.Lock()
	defer s.publishMu.Unlock()
	logins, proxyURLs, err := s.collectMeta(ctx)
	if err != nil {
		return err
	}
	if err := connection.Send(map[string]any{"op": operation.OpcodeReady, "body": map[string]any{"logins": logins, "proxy_urls": proxyURLs}}); err != nil {
		return err
	}
	go connection.Heartbeat(defaultHeartbeat)
	if sequence >= 0 {
		for _, evt := range s.eventCache.After(sequence) {
			if isLoginEventType(evt.kind) {
				continue
			}
			if err := connection.Send(map[string]any{"op": operation.OpcodeEvent, "body": evt.body}); err != nil {
				return err
			}
		}
	}
	s.addConnection(connection)
	return nil
}

// authorized protects Satori RPC and metadata routes, not platform callbacks or static files.
func (s *Server) authorized(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if s.authorize(w, r) {
			next(w, r)
		}
	}
}

func (s *Server) authorize(w http.ResponseWriter, r *http.Request) bool {
	if s.Token == "" {
		return true
	}
	token, ok := protocol.ParseBearer(r.Header.Get(protocol.HeaderAuthorization))
	if !ok || token != s.Token {
		s.log(r.Context(), LogLevelWarn, "Rejected an HTTP request because Satori API authorization failed.")
		w.Header().Set("WWW-Authenticate", "Bearer")
		writeError(w, Unauthorized("invalid Satori authorization"))
		return false
	}
	return true
}

func (s *Server) httpServerHandler(w http.ResponseWriter, request *http.Request) {
	s.ensureDefaultUploadRoute()

	action := s.extractAction(request)
	if protocol.IsApi(protocol.ParseApi(action)) && request.Method != http.MethodPost {
		writeMethodNotAllowed(w)
		return
	}
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

	// Public GET resources contain opaque identifiers. Native API proxying always
	// uses Satori authorization, including GET requests that may disclose platform data.
	normalized, parseErr := normalizeProxyURL(rawURL)
	if parseErr != nil {
		writeError(w, BadRequest(parseErr.Error()))
		return
	}
	match := internalURLPattern.FindStringSubmatch(normalized)
	native := false
	if len(match) == 4 {
		nativePath, _, _ := strings.Cut(match[3], "?")
		native = nativePath == "_api" || strings.HasPrefix(nativePath, "_api/")
	}
	if (native || (request.Method != http.MethodGet && request.Method != http.MethodHead)) && !s.authorize(w, request) {
		return
	}
	if request.URL.RawQuery != "" {
		separator := "?"
		if strings.Contains(normalized, "?") {
			separator = "&"
		}
		normalized += separator + request.URL.RawQuery
	}
	resp, err := s.fetchProxy(normalized, request)
	if err != nil {
		writeError(w, err)
		return
	}

	if resp == nil {
		writeError(w, NewActionError(http.StatusInternalServerError, "empty proxy response", nil))
		return
	}

	chunkSize := 0
	if resp.Stream != nil || len(resp.Body) > s.streamThreshold {
		chunkSize = s.streamChunkSize
	}
	if err := writeServerResponse(w, resp, chunkSize); err != nil {
		s.log(request.Context(), LogLevelError, fmt.Sprintf("Failed to write the resource response to the client: %s", logging.ErrorText(err)))
	}
}

func (s *Server) executeRoute(
	w http.ResponseWriter,
	request *http.Request,
	action string,
	platform string,
	selfID string,
	handler RouteCall[any, any],
) {
	started := time.Now()
	status := http.StatusOK
	defer func() {
		label := action
		if strings.HasPrefix(label, protocol.InternalApiPrefix) {
			label = "internal"
		}
		level := LogLevelDebug
		if status >= 400 {
			level = LogLevelWarn
		}
		if status >= 500 {
			level = LogLevelError
		}
		description := fmt.Sprintf("Satori request %s for %s bot %s ended with HTTP %d after %d ms.", logging.SafeText(label), logging.SafeText(platform), logging.SafeText(selfID), status, time.Since(started).Milliseconds())
		if status >= 200 && status < 300 {
			description = fmt.Sprintf("Handled Satori request %s for %s bot %s in %d ms (HTTP %d).", logging.SafeText(label), logging.SafeText(platform), logging.SafeText(selfID), time.Since(started).Milliseconds(), status)
		}
		s.log(request.Context(), level, description)
	}()
	if !strings.HasPrefix(action, protocol.InternalApiPrefix) {
		request.Body = http.MaxBytesReader(w, request.Body, s.maxRequestBytes)
	}
	params, err := parseParams(action, request)
	if err != nil {
		status = statusFromError(err)
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
		status = statusFromError(callErr)
		writeError(w, callErr)
		return
	}

	switch typed := result.(type) {
	case *Response:
		status = typed.statusCodeOrDefault()
		if err := writeServerResponse(w, typed, 0); err != nil {
			s.log(request.Context(), LogLevelError, fmt.Sprintf("Failed to write the response to the client: %s", logging.ErrorText(err)))
		}
	case Response:
		status = typed.statusCodeOrDefault()
		if err := writeServerResponse(w, &typed, 0); err != nil {
			s.log(request.Context(), LogLevelError, fmt.Sprintf("Failed to write the response to the client: %s", logging.ErrorText(err)))
		}
	default:
		var deliveryErr error
		status, deliveryErr = writeJSON(w, http.StatusOK, typed)
		if deliveryErr != nil {
			s.log(request.Context(), LogLevelError, fmt.Sprintf("Failed to deliver the JSON response for Satori request %s: %s", logging.SafeText(action), logging.ErrorText(deliveryErr)))
		}
	}
}

func (s *Server) findRouteHandler(action string, platform string, selfID string) (RouteCall[any, any], bool) {
	s.mu.RLock()
	adapters := append([]Adapter(nil), s.adapters...)
	serverRoutes := copyRouteMap(s.routes)
	routers := append([]Router(nil), s.routers...)
	s.mu.RUnlock()

	var selected RouteCall[any, any]
	for _, adapter := range adapters {
		handler, ok := matchRoute(adapter.Routes(), action)
		if !ok {
			continue
		}
		if !adapter.Ensure(platform, selfID) {
			continue
		}
		if selected != nil {
			return func(*Request[any]) (any, error) {
				return nil, NewActionError(409, "ambiguous platform account route", nil)
			}, true
		}
		selected = handler
	}
	if selected != nil {
		return selected, true
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

// rawURL has already been decoded at the proxy route boundary. Query bytes
// retain their own URL encoding when the target request is constructed.
func (s *Server) fetchProxy(rawURL string, request *http.Request) (*Response, error) {
	if strings.HasPrefix(rawURL, "internal:") {
		return s.fetchInternalProxy(rawURL, request)
	}
	ctx := context.Background()
	if request != nil {
		ctx = request.Context()
	}
	return s.fetchExternalProxy(ctx, rawURL)
}

func (s *Server) fetchInternalProxy(rawURL string, request *http.Request) (*Response, error) {
	match := internalURLPattern.FindStringSubmatch(rawURL)
	if len(match) != 4 {
		return nil, BadRequest(fmt.Sprintf("invalid internal url: %s", rawURL))
	}

	platform := match[1]
	selfID := match[2]
	path := match[3]
	operationPath, query, _ := strings.Cut(path, "?")
	if request != nil && (operationPath == "_api" || strings.HasPrefix(operationPath, "_api/")) {
		// Match the ordinary /internal request boundary: path and raw query
		// are distinct, so the provider sends the query exactly once.
		request = request.Clone(request.Context())
		request.URL.RawQuery = query
		path = operationPath
	}
	if strings.ContainsAny(platform+selfID, "\\?#\x00") {
		return nil, BadRequest("invalid internal identity")
	}
	if !s.hasLogin(platform, selfID) {
		return nil, NotFound("resource login not found")
	}
	reserved := strings.SplitN(path, "/", 2)[0]
	if strings.HasPrefix(reserved, "_") && reserved != "_tmp" && reserved != "_api" {
		return nil, NotFound("unknown reserved resource path")
	}
	hasTmpPrefix := reserved == "_tmp"
	tmpPath := strings.TrimPrefix(path, "_tmp/")
	if hasTmpPrefix {
		if !strings.HasPrefix(path, "_tmp/") {
			return nil, BadRequest("temporary file identifier is required")
		}
		resp, err := s.fetchTempFile(platform, selfID, tmpPath)
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

func (s *Server) fetchExternalProxy(ctx context.Context, rawURL string) (*Response, error) {
	u, err := url.Parse(rawURL)
	if err != nil || u.Host == "" || u.User != nil || (u.Scheme != "http" && u.Scheme != "https") {
		return nil, BadRequest("invalid external resource URL")
	}
	for _, provider := range s.snapshotProviders() {
		for _, prefix := range provider.ProxyUrls() {
			if !strings.HasPrefix(rawURL, prefix) {
				continue
			}
			resp, err := provider.HandleProxied(ctx, prefix, rawURL)
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

func (s *Server) fetchTempFile(platform, selfID, name string) (*Response, error) {
	if name == "" || name == "." || name == ".." || strings.ContainsAny(name, "/\\:\x00?#") {
		return nil, BadRequest("invalid temporary file identifier")
	}
	s.mu.RLock()
	record, dir := s.tempFiles[name], s.tempDir
	s.mu.RUnlock()
	if record == nil || dir == "" || !time.Now().Before(record.expires) {
		return nil, NotFound("temporary resource expired or not found")
	}
	if record.platform != platform || record.selfID != selfID {
		return nil, Forbidden("temporary resource belongs to a different login")
	}
	file, err := os.Open(filepath.Join(dir, name))
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, NotFound("temporary resource not found")
		}
		return nil, err
	}
	info, err := file.Stat()
	if err != nil {
		file.Close()
		return nil, err
	}
	response := NewStreamResponse(http.StatusOK, file)
	response.Header.Set("Content-Type", record.contentType)
	response.ContentLength = info.Size()
	return response, nil
}

func (s *Server) removeTemporaryFile(name string) {
	s.mu.Lock()
	record, dir := s.tempFiles[name], s.tempDir
	delete(s.tempFiles, name)
	if record != nil && record.timer != nil {
		record.timer.Stop()
	}
	s.mu.Unlock()
	if record != nil && dir != "" {
		_ = os.Remove(filepath.Join(dir, name))
	}
}

func (s *Server) defaultUploadCreateHandler(request *Request[UploadCreateParam]) (map[string]string, error) {
	if request == nil || request.Params == nil {
		return nil, BadRequest("invalid form data")
	}
	if !s.hasLogin(request.Platform, request.SelfID) {
		return nil, NotFound("upload login not found")
	}
	result := map[string]string{}
	var total int64
	for name, file := range request.Params {
		if name == "" {
			return nil, BadRequest("upload name is required")
		}
		contentType := file.ContentType
		if int64(len(file.Data)) > s.maxRequestBytes-total {
			return nil, NewActionError(413, "upload exceeds local request limit", nil)
		}
		if request.Origin != nil {
			if err := request.Origin.Context().Err(); err != nil {
				return nil, err
			}
		}
		token, err := randomToken(16)
		if err != nil {
			return nil, err
		}
		filename := filepath.Base(strings.ReplaceAll(file.Filename, "\\", "/"))
		if filename == "" || filename == "." || filename == ".." {
			filename = "upload"
		}
		if strings.ContainsAny(filename, ":\x00?#") {
			return nil, BadRequest("invalid upload filename")
		}
		finalName := token + "-" + filename
		s.mu.RLock()
		dir := s.tempDir
		s.mu.RUnlock()
		if dir == "" {
			return nil, errors.New("temporary directory is closed")
		}
		target := filepath.Join(dir, finalName)
		output, err := os.OpenFile(target, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
		if err != nil {
			return nil, err
		}
		size, copyErr := io.Copy(output, bytes.NewReader(file.Data))
		closeErr := output.Close()
		if err = errors.Join(copyErr, closeErr); err != nil {
			os.Remove(target)
			return nil, err
		}
		total += size
		if total > s.maxRequestBytes {
			os.Remove(target)
			return nil, NewActionError(413, "upload exceeds local request limit", nil)
		}
		record := &temporaryFile{platform: request.Platform, selfID: request.SelfID, contentType: contentType, expires: time.Now().Add(10 * time.Minute)}
		s.mu.Lock()
		if s.tempDir != dir {
			s.mu.Unlock()
			os.Remove(target)
			return nil, errors.New("temporary directory is closed")
		}
		record.timer = time.AfterFunc(10*time.Minute, func() { s.removeTemporaryFile(finalName) })
		s.tempFiles[finalName] = record
		s.mu.Unlock()
		result[name] = fmt.Sprintf("internal:%s/%s/_tmp/%s", request.Platform, request.SelfID, finalName)
	}
	s.log(context.Background(), LogLevelDebug, fmt.Sprintf("Accepted %d uploaded files totaling %d bytes for %s bot %s.", len(result), total, logging.SafeText(request.Platform), logging.SafeText(request.SelfID)))
	return result, nil
}

func (s *Server) collectLogins(ctx context.Context) ([]*login.Login, error) {
	providers := s.snapshotProviders()
	snapshots := make([][]*login.Login, len(providers))
	for index, provider := range providers {
		values, err := provider.GetLogins(ctx)
		if err != nil {
			return nil, err
		}
		seen := map[int64]bool{}
		for _, info := range values {
			if info == nil || info.Sn < 0 || seen[info.Sn] {
				return nil, errors.New("invalid or duplicate provider login sequence")
			}
			seen[info.Sn] = true
			snapshots[index] = append(snapshots[index], info.Clone())
		}
	}
	result := make([]*login.Login, 0)
	s.mu.Lock()
	defer s.mu.Unlock()
	for index, values := range snapshots {
		for _, info := range values {
			binding := s.bindLoginLocked(index+1, info)
			binding.info = info.Clone()
			value := info.Clone()
			value.Sn = binding.sn
			result = append(result, value)
		}
	}
	return result, nil
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
	body := map[string]any{"proxy_urls": s.getProxyURLs()}
	s.mu.RLock()
	webhooks := append([]WebhookEndpoint(nil), s.webhooks...)
	s.mu.RUnlock()
	var result error
	for _, hook := range webhooks {
		if err := s.sendWebhook(ctx, hook, operation.OpcodeMeta, body); err != nil {
			result = errors.Join(result, err)
		}
	}
	return result
}

func (s *Server) sendWebhook(ctx context.Context, webhook WebhookEndpoint, opcode operation.Opcode, body any) error {
	payload, err := json.Marshal(body)
	if err != nil {
		return err
	}

	timeout := webhook.Timeout
	if timeout <= 0 {
		timeout = defaultWebhookTimeout
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, webhook.URL, bytes.NewReader(payload))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	if webhook.Token != "" {
		protocol.SetBearer(req.Header, webhook.Token)
	}
	protocol.SetOpcode(req.Header, int(opcode))

	resp, err := s.httpClient.Do(req)
	if err != nil {
		s.log(ctx, LogLevelError, fmt.Sprintf("Failed to send a %s frame to Webhook client %s: %s", logging.FrameName(opcode), logging.Endpoint(webhook.URL), logging.ErrorText(err)))
		return err
	}
	defer resp.Body.Close()
	level := LogLevelDebug
	if resp.StatusCode >= 400 {
		level = LogLevelWarn
	}
	if resp.StatusCode >= 500 {
		level = LogLevelError
	}
	endpoint, frame := logging.Endpoint(webhook.URL), logging.FrameName(opcode)
	description := fmt.Sprintf("Webhook client %s returned HTTP %d for the %s frame.", endpoint, resp.StatusCode, frame)
	switch {
	case resp.StatusCode >= 200 && resp.StatusCode < 300:
		description = fmt.Sprintf("Webhook client %s accepted the %s frame (HTTP %d).", endpoint, frame, resp.StatusCode)
	case resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden:
		description = fmt.Sprintf("Webhook client %s rejected authorization for the %s frame (HTTP %d).", endpoint, frame, resp.StatusCode)
	case resp.StatusCode >= 500:
		description = fmt.Sprintf("Webhook client %s returned a server error for the %s frame (HTTP %d).", endpoint, frame, resp.StatusCode)
	}
	s.log(ctx, level, description)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
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

func (s *Server) log(ctx context.Context, level LogLevel, v ...any) {
	s.mu.RLock()
	logger := s.logger
	s.mu.RUnlock()
	if logger == nil {
		return
	}
	logger.Log(ctx, level, v...)
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
		s.log(ctx, LogLevelError, fmt.Sprintf("Failed to build the Satori HTTP handler: %s", logging.ErrorText(err)))
		return err
	}

	addr := net.JoinHostPort(s.Host, strconv.Itoa(s.Port))
	listener, err := net.Listen("tcp", addr)
	if err != nil {
		s.log(ctx, LogLevelError, fmt.Sprintf("Failed to listen on %s: %s", addr, logging.ErrorText(err)))
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

	for index, provider := range s.snapshotProviders() {
		publisher, ok := provider.(EventPublisher)
		if !ok {
			continue
		}
		stream := publisher.Publisher(groupCtx)
		source := index + 1
		group.Go(func() error {
			err := s.runPublisherTask(groupCtx, source, stream)
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

	if err := s.broadcastMetaToWebhooks(groupCtx); err != nil {
		s.log(ctx, LogLevelWarn, fmt.Sprintf("Failed to send metadata to one or more Webhook clients: %s", logging.ErrorText(err)))
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

func (s *Server) runPublisherTask(ctx context.Context, source int, stream <-chan *event.Event) error {
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
			if err := s.postFrom(ctx, source, evt); err != nil {
				s.log(ctx, LogLevelError, fmt.Sprintf("Failed to deliver an event from publisher %d: %s", source, logging.ErrorText(err)))
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
	for _, file := range s.tempFiles {
		if file.timer != nil {
			file.timer.Stop()
		}
	}
	s.tempFiles = map[string]*temporaryFile{}
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
	if _, exists := s.routes[string(protocol.ApiUploadCreate)]; exists {
		return
	}
	s.routes[string(protocol.ApiUploadCreate)] = Wrapper(s.defaultUploadCreateHandler)
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
		Op   *operation.Opcode `json:"op"`
		Body struct {
			Token    string `json:"token"`
			Sn       *int64 `json:"sn"`
			Sequence *int64 `json:"sequence"`
		} `json:"body"`
	}
	if err := json.Unmarshal(payload, &frame); err != nil {
		return "", -1, err
	}
	if frame.Op == nil || *frame.Op != operation.OpcodeIdentify {
		return "", -1, errors.New("invalid identify opcode")
	}
	position := frame.Body.Sn
	if position == nil {
		position = frame.Body.Sequence
	}
	sequence := int64(-1)
	if position != nil {
		// Preserve the existing negative sentinel for clients that explicitly opt out of replay.
		sequence = *position
	}
	return frame.Body.Token, sequence, nil
}

func parseParams(action string, request *http.Request) (any, error) {
	// Native handlers receive the original method, query and body via Origin.
	if strings.HasPrefix(action, protocol.InternalApiPrefix) {
		return nil, nil
	}
	if action == string(protocol.ApiUploadCreate) {
		return parseUploads(request)
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
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			return nil, NewActionError(413, "request exceeds local limit", err)
		}
		return nil, err
	}
	var params any
	decoded, err := protocol.DecodeJSONBytes(body, &params)
	if err != nil {
		return nil, BadRequest(err.Error())
	}
	if !decoded {
		return map[string]any{}, nil
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
	if err := protocol.DecodeJSONReaderRequired(reader, dst); err != nil {
		if errors.Is(err, protocol.ErrEmptyJSONBody) {
			return errors.New("empty request body")
		}
		return err
	}
	return nil
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
	for key, values := range ErrorHeaders(err) {
		for _, value := range values {
			w.Header().Add(key, value)
		}
	}
	http.Error(w, err.Error(), status)
}

func writeJSON(w http.ResponseWriter, status int, payload any) (int, error) {
	data, err := json.Marshal(payload)
	if err != nil {
		writeError(w, err)
		return statusFromError(err), err
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	n, err := w.Write(data)
	if err == nil && n != len(data) {
		err = io.ErrShortWrite
	}
	return status, err
}

func writeServerResponse(w http.ResponseWriter, response *Response, chunkSize int) error {
	if response == nil {
		w.WriteHeader(http.StatusOK)
		return nil
	}
	for key, values := range protocol.ForwardHeaders(response.Header) {
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
		flusher, _ := w.(http.Flusher)
		for {
			n, err := response.Stream.Read(buffer)
			if n > 0 {
				if written, writeErr := w.Write(buffer[:n]); writeErr != nil {
					return writeErr
				} else if written != n {
					return io.ErrShortWrite
				}
				if flusher != nil {
					flusher.Flush()
				}
			}
			if err == io.EOF {
				return nil
			}
			if err != nil {
				return err
			}
		}
	}
	if len(response.Body) == 0 {
		return nil
	}
	if chunkSize <= 0 {
		chunkSize = len(response.Body)
	}
	for offset := 0; offset < len(response.Body); offset += chunkSize {
		end := offset + chunkSize
		if end > len(response.Body) {
			end = len(response.Body)
		}
		if written, err := w.Write(response.Body[offset:end]); err != nil {
			return err
		} else if written != end-offset {
			return io.ErrShortWrite
		}
	}
	return nil
}

func extractPlatformAndSelfID(header http.Header) (string, string, error) {
	platform, selfID, err := protocol.ExtractIdentityHeaders(header)
	if err != nil {
		return "", "", BadRequest(err.Error())
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

func cloneHTTPHeader(source http.Header) http.Header {
	if source == nil {
		return http.Header{}
	}
	cloned := http.Header{}
	for key, values := range source {
		if len(values) == 0 {
			continue
		}
		copied := make([]string, 0, len(values))
		copied = append(copied, values...)
		cloned[key] = copied
	}
	return cloned
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
