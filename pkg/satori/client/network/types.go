package network

import (
	"context"
	"sync"
	"sync/atomic"
	"time"

	"github.com/satori-protocol-go/satori-go/pkg/satori/model/event"
	"github.com/satori-protocol-go/satori-go/pkg/satori/model/login"
)

type APIConfig interface {
	APIBase() string
	TokenValue() string
	TimeoutValue() time.Duration
}

type AppBridge interface {
	SyncLogins(networkID string, cfg APIConfig, proxyURLs []string, logins []*login.Login)
	PostEvent(networkID string, evt *event.Event)
	MarkNetworkStatus(networkID string, status login.LoginStatus, remove bool)
}

type Runner interface {
	ID() string
	Run(ctx context.Context) error
	Close() error
}

type base struct {
	id     string
	app    AppBridge
	config APIConfig

	sequence atomic.Int64

	mu        sync.RWMutex
	proxyURLs []string

	closeSignal chan struct{}
	closeOnce   sync.Once
}

func newBase(app AppBridge, cfg APIConfig, id string) *base {
	result := &base{
		id:          id,
		app:         app,
		config:      cfg,
		closeSignal: make(chan struct{}),
	}
	result.sequence.Store(-1)
	return result
}

func (b *base) ID() string {
	return b.id
}

func (b *base) Config() APIConfig {
	return b.config
}

func (b *base) Sequence() int64 {
	return b.sequence.Load()
}

func (b *base) SetSequence(sequence int64) {
	b.sequence.Store(sequence)
}

func (b *base) ProxyURLs() []string {
	b.mu.RLock()
	defer b.mu.RUnlock()
	copied := make([]string, len(b.proxyURLs))
	copy(copied, b.proxyURLs)
	return copied
}

func (b *base) SetProxyURLs(proxyURLs []string) {
	b.mu.Lock()
	b.proxyURLs = append([]string(nil), proxyURLs...)
	b.mu.Unlock()
}

func (b *base) CloseSignal() <-chan struct{} {
	return b.closeSignal
}

func (b *base) MarkClosed() {
	b.closeOnce.Do(func() {
		close(b.closeSignal)
	})
}
