package client

import (
	"fmt"
	"path"
	"strings"
	"time"
)

const (
	defaultHost    = "localhost"
	defaultPort    = 5140
	defaultVersion = "v1"

	defaultWebhookHost = "127.0.0.1"
	defaultWebhookPort = 8080
	defaultWebhookPath = "v1/events"
)

type APIConfig interface {
	APIBase() string
	TokenValue() string
	TimeoutValue() time.Duration
}

type Config interface {
	APIConfig
	Identity() string
	NetworkKind() string
}

type WebSocketConfig struct {
	Host             string
	Port             int
	Path             string
	Version          string
	Token            string
	Timeout          time.Duration
	HandshakeTimeout time.Duration
}

// WebsocketsInfo keeps naming parity with satori-python.
type WebsocketsInfo = WebSocketConfig

func (c *WebSocketConfig) normalize() {
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

func (c WebSocketConfig) NetworkKind() string {
	return "ws"
}

func (c WebSocketConfig) Identity() string {
	host := c.Host
	if strings.TrimSpace(host) == "" {
		host = defaultHost
	}
	port := c.Port
	if port == 0 {
		port = defaultPort
	}
	return fmt.Sprintf("%s:%d", host, port)
}

func (c WebSocketConfig) APIBase() string {
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
	return fmt.Sprintf("http://%s:%d%s/%s", host, port, normalizeLeadingPath(c.Path), version)
}

func (c WebSocketConfig) WSBase() string {
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
	return fmt.Sprintf("ws://%s:%d%s/%s", host, port, normalizeLeadingPath(c.Path), version)
}

func (c WebSocketConfig) TokenValue() string {
	return c.Token
}

func (c WebSocketConfig) TimeoutValue() time.Duration {
	return c.Timeout
}

type WebhookConfig struct {
	Host       string
	Port       int
	Path       string
	Token      string
	ServerHost string
	ServerPort int
	ServerPath string
	Version    string
	Timeout    time.Duration
}

// WebhookInfo keeps naming parity with satori-python.
type WebhookInfo = WebhookConfig

func (c *WebhookConfig) normalize() {
	if strings.TrimSpace(c.Host) == "" {
		c.Host = defaultWebhookHost
	}
	if c.Port == 0 {
		c.Port = defaultWebhookPort
	}
	if strings.TrimSpace(c.Path) == "" {
		c.Path = defaultWebhookPath
	}
	c.Path = normalizeLeadingPath(c.Path)

	if strings.TrimSpace(c.ServerHost) == "" {
		c.ServerHost = defaultHost
	}
	if c.ServerPort == 0 {
		c.ServerPort = defaultPort
	}
	if strings.TrimSpace(c.Version) == "" {
		c.Version = defaultVersion
	}
	c.ServerPath = normalizeLeadingPath(c.ServerPath)
}

func (c WebhookConfig) NetworkKind() string {
	return "webhook"
}

func (c WebhookConfig) Identity() string {
	host := c.Host
	if strings.TrimSpace(host) == "" {
		host = defaultWebhookHost
	}
	port := c.Port
	if port == 0 {
		port = defaultWebhookPort
	}
	p := c.Path
	if strings.TrimSpace(p) == "" {
		p = "/" + defaultWebhookPath
	} else {
		p = normalizeLeadingPath(p)
	}
	return fmt.Sprintf("%s:%d%s", host, port, p)
}

func (c WebhookConfig) APIBase() string {
	host := c.ServerHost
	if strings.TrimSpace(host) == "" {
		host = defaultHost
	}
	port := c.ServerPort
	if port == 0 {
		port = defaultPort
	}
	version := strings.TrimSpace(c.Version)
	if version == "" {
		version = defaultVersion
	}
	return fmt.Sprintf("http://%s:%d%s/%s", host, port, normalizeLeadingPath(c.ServerPath), version)
}

func (c WebhookConfig) CallbackURL() string {
	host := c.Host
	if strings.TrimSpace(host) == "" {
		host = defaultWebhookHost
	}
	port := c.Port
	if port == 0 {
		port = defaultWebhookPort
	}
	p := c.Path
	if strings.TrimSpace(p) == "" {
		p = defaultWebhookPath
	}
	p = normalizeLeadingPath(p)
	return fmt.Sprintf("http://%s:%d%s", host, port, p)
}

func (c WebhookConfig) TokenValue() string {
	return c.Token
}

func (c WebhookConfig) TimeoutValue() time.Duration {
	return c.Timeout
}

func normalizeLeadingPath(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	if !strings.HasPrefix(raw, "/") {
		raw = "/" + raw
	}
	return strings.TrimSuffix(raw, "/")
}

func joinURLPath(base string, segments ...string) string {
	cleaned := make([]string, 0, len(segments))
	for _, segment := range segments {
		segment = strings.TrimSpace(segment)
		if segment == "" {
			continue
		}
		cleaned = append(cleaned, segment)
	}
	if len(cleaned) == 0 {
		return strings.TrimSuffix(base, "/")
	}
	return strings.TrimSuffix(base, "/") + "/" + strings.TrimPrefix(path.Join(cleaned...), "/")
}
