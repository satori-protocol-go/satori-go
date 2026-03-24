package client

import (
	"context"
	"fmt"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/satori-protocol-go/satori-go/pkg/satori/model/login"
)

type APIInfo struct {
	Host    string
	Port    int
	Path    string
	Version string
	Token   string
	Secure  bool
	Timeout time.Duration
}

// ApiInfo keeps naming parity with satori-python.
type ApiInfo = APIInfo

func (c *APIInfo) normalize() {
	if strings.TrimSpace(c.Host) == "" {
		c.Host = defaultHost
	}
	if c.Port == 0 {
		c.Port = defaultPort
	}
	if strings.TrimSpace(c.Version) == "" {
		c.Version = defaultVersion
	}
	c.Path = normalizeLeadingPath(c.Path)
}

func (c APIInfo) APIBase() string {
	host := c.Host
	if strings.TrimSpace(host) == "" {
		host = defaultHost
	}
	port := c.Port
	if port == 0 {
		port = defaultPort
	}
	version := strings.TrimSpace(c.Version)
	if version == "" {
		version = defaultVersion
	}
	return fmt.Sprintf("%s://%s:%d%s/%s", httpScheme(c.Secure), host, port, normalizeLeadingPath(c.Path), version)
}

func (c APIInfo) TokenValue() string {
	return c.Token
}

func (c APIInfo) TimeoutValue() time.Duration {
	return c.Timeout
}

type ProtocolFactory func(*Account) *APIProtocol

type Account struct {
	*APIProtocol

	Adapter  string
	SelfInfo *login.Login
	Config   APIConfig
	Protocol *APIProtocol

	mu        sync.RWMutex
	proxyURLs []string
	connected bool
	ready     chan struct{}
}

func NewAccount(selfInfo *login.Login, cfg APIConfig, proxyURLs []string, protocolFactory ProtocolFactory) *Account {
	if selfInfo == nil {
		selfInfo = &login.Login{}
	}
	account := &Account{
		Adapter:  selfInfo.Adapter,
		SelfInfo: selfInfo,
		Config:   cfg,
		ready:    make(chan struct{}),
	}
	account.SetProxyURLs(proxyURLs)
	if protocolFactory == nil {
		protocolFactory = func(acc *Account) *APIProtocol {
			return NewAPIProtocol(acc, nil)
		}
	}
	protocol := protocolFactory(account)
	if protocol == nil {
		protocol = NewAPIProtocol(account, nil)
	}
	account.APIProtocol = protocol
	account.Protocol = protocol
	return account
}

func (a *Account) Platform() string {
	a.mu.RLock()
	defer a.mu.RUnlock()
	if a.SelfInfo == nil || strings.TrimSpace(a.SelfInfo.Platform) == "" {
		return "satori"
	}
	return a.SelfInfo.Platform
}

func (a *Account) SelfID() string {
	a.mu.RLock()
	defer a.mu.RUnlock()
	if a.SelfInfo == nil || a.SelfInfo.User == nil {
		return ""
	}
	return a.SelfInfo.User.Id
}

func (a *Account) Connected() bool {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.connected
}

func (a *Account) SetConnected(connected bool) {
	a.mu.Lock()
	defer a.mu.Unlock()

	if connected {
		if a.connected {
			return
		}
		if a.ready == nil {
			a.ready = make(chan struct{})
		}
		close(a.ready)
		a.connected = true
		return
	}

	if !a.connected {
		if a.ready == nil {
			a.ready = make(chan struct{})
		}
		return
	}
	a.connected = false
	a.ready = make(chan struct{})
}

func (a *Account) WaitConnected(ctx context.Context, interval time.Duration) error {
	_ = interval
	if ctx == nil {
		ctx = context.Background()
	}

	a.mu.RLock()
	if a.connected {
		a.mu.RUnlock()
		return nil
	}
	ready := a.ready
	a.mu.RUnlock()

	if ready == nil {
		a.mu.Lock()
		if a.ready == nil {
			a.ready = make(chan struct{})
		}
		ready = a.ready
		a.mu.Unlock()
	}

	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-ready:
		return nil
	}
}

func (a *Account) ProxyURLs() []string {
	a.mu.RLock()
	defer a.mu.RUnlock()
	copied := make([]string, len(a.proxyURLs))
	copy(copied, a.proxyURLs)
	return copied
}

func (a *Account) SetProxyURLs(urls []string) {
	a.mu.Lock()
	a.proxyURLs = append([]string(nil), urls...)
	a.mu.Unlock()
}

func (a *Account) EnsureURL(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}

	if strings.HasPrefix(raw, "internal:") {
		return joinURLPath(a.Config.APIBase(), "proxy", url.PathEscape(strings.TrimLeft(raw, "/")))
	}

	for _, prefix := range a.ProxyURLs() {
		if strings.HasPrefix(raw, prefix) {
			return joinURLPath(a.Config.APIBase(), "proxy", url.PathEscape(strings.TrimLeft(raw, "/")))
		}
	}

	parsed, err := url.Parse(raw)
	if err == nil && parsed.Scheme != "" {
		return parsed.String()
	}
	return "http://" + strings.TrimPrefix(raw, "//")
}

func (a *Account) Custom(config APIConfig, protocolFactory ProtocolFactory) *Account {
	options := []CustomOption{}
	if config != nil {
		options = append(options, WithCustomConfig(config))
	}
	if protocolFactory != nil {
		options = append(options, WithCustomProtocolFactory(protocolFactory))
	}
	return a.CustomWith(options...)
}

func (a *Account) CustomWith(options ...CustomOption) *Account {
	settings := &customOptions{
		config: a.Config,
	}
	for _, option := range options {
		if option == nil {
			continue
		}
		option(settings)
	}

	config := settings.config
	if settings.apiInfo != nil {
		settings.apiInfo.normalize()
		config = *settings.apiInfo
	}
	if config == nil {
		config = a.Config
	}
	return NewAccount(a.SelfInfo, config, a.ProxyURLs(), settings.protocolFactory)
}

func (a *Account) String() string {
	return fmt.Sprintf("<Account %s (%s)>", a.SelfID(), a.Platform())
}
