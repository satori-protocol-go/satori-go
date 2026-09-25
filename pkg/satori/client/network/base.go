package network

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gorilla/websocket"
	"github.com/satori-protocol-go/satori-go/pkg/satori/logging"
	"github.com/satori-protocol-go/satori-go/pkg/satori/model/event"
	"github.com/satori-protocol-go/satori-go/pkg/satori/model/login"
	"github.com/satori-protocol-go/satori-go/pkg/satori/protocol"
)

type APIConfig interface {
	APIBase() string
	TokenValue() string
	TimeoutValue() time.Duration
}

type AppBridge interface {
	UpdateProxyURLs(networkID string, proxyURLs []string)
	SyncLogins(networkID string, cfg APIConfig, proxyURLs []string, logins []*login.Login) error
	PostEvent(networkID string, evt *event.Event) error
	MarkNetworkStatus(networkID string, status login.LoginStatus, remove bool)
}

type Runner interface {
	ID() string
	Run(ctx context.Context) error
	Close() error
}

type Availability interface {
	Alive() bool
	WaitForAvailable(ctx context.Context) error
}

type LoggerSetter interface {
	SetLogger(logger logging.Logger)
}

type WebSocketOptions struct {
	Identity         string
	WSBase           string
	Token            string
	APIConfig        APIConfig
	Dialer           *websocket.Dialer
	HandshakeTimeout time.Duration
	Logger           logging.Logger
}

type WebhookOptions struct {
	Identity  string
	Host      string
	Port      int
	Path      string
	Token     string
	APIConfig APIConfig
	Timeout   time.Duration
	Logger    logging.Logger
}

func webhookAddress(host string, port int) string {
	return fmt.Sprintf("%s:%d", host, port)
}

func normalizedTimeout(timeout time.Duration) time.Duration {
	if timeout <= 0 {
		return protocol.DefaultRequestTimeout
	}
	return timeout
}

type baseNetwork struct {
	id     string
	app    AppBridge
	config APIConfig
	logger logging.Logger

	sequence atomic.Int64

	mu        sync.RWMutex
	proxyURLs []string

	closeSignal chan struct{}
	closeOnce   sync.Once

	availableSignal chan struct{}
	available       bool
}

func newBaseNetwork(app AppBridge, cfg APIConfig, id string, logger logging.Logger) *baseNetwork {
	if logger == nil {
		logger = logging.NopLogger{}
	}
	result := &baseNetwork{
		id:              id,
		app:             app,
		config:          cfg,
		logger:          logger,
		closeSignal:     make(chan struct{}),
		availableSignal: make(chan struct{}),
	}
	result.sequence.Store(-1)
	return result
}

func (b *baseNetwork) ID() string {
	return b.id
}

func (b *baseNetwork) Config() APIConfig {
	return b.config
}

func (b *baseNetwork) Sequence() int64 {
	return b.sequence.Load()
}

func (b *baseNetwork) SetSequence(sequence int64) {
	b.sequence.Store(sequence)
}

func (b *baseNetwork) ProxyURLs() []string {
	b.mu.RLock()
	defer b.mu.RUnlock()
	copied := make([]string, len(b.proxyURLs))
	copy(copied, b.proxyURLs)
	return copied
}

func (b *baseNetwork) SetProxyURLs(proxyURLs []string) {
	b.mu.Lock()
	b.proxyURLs = append([]string(nil), proxyURLs...)
	b.mu.Unlock()
}

func (b *baseNetwork) CloseSignal() <-chan struct{} {
	return b.closeSignal
}

func (b *baseNetwork) MarkClosed() {
	b.closeOnce.Do(func() { close(b.closeSignal) })
	b.MarkUnavailable()
}

func (b *baseNetwork) MarkAvailable() {
	b.mu.Lock()
	defer b.mu.Unlock()
	select {
	case <-b.closeSignal:
		return
	default:
	}
	if !b.available {
		b.available = true
		close(b.availableSignal)
	}
}

func (b *baseNetwork) MarkUnavailable() {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.available {
		b.available = false
		b.availableSignal = make(chan struct{})
	}
}

func (b *baseNetwork) Available() bool {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return b.available
}

func (b *baseNetwork) WaitAvailable(ctx context.Context) error {
	if ctx == nil {
		ctx = context.Background()
	}
	for {
		select {
		case <-b.closeSignal:
			return context.Canceled
		default:
		}
		b.mu.RLock()
		ready, signal := b.available, b.availableSignal
		b.mu.RUnlock()
		if ready {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-b.closeSignal:
			return context.Canceled
		case <-signal:
		}
	}
}

func (b *baseNetwork) Log(ctx context.Context, level logging.Level, v ...any) {
	if b == nil {
		return
	}
	b.mu.RLock()
	logger := b.logger
	b.mu.RUnlock()
	if logger != nil {
		logger.Log(ctx, level, v...)
	}
}

func (b *baseNetwork) SetLogger(logger logging.Logger) {
	if b == nil {
		return
	}
	if logger == nil {
		logger = logging.NopLogger{}
	}
	b.mu.Lock()
	b.logger = logger
	b.mu.Unlock()
}
