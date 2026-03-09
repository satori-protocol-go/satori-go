package network

import (
	"fmt"
	"time"

	"github.com/gorilla/websocket"
)

type WebSocketOptions struct {
	Identity  string
	WSBase    string
	Token     string
	APIConfig APIConfig
	Dialer    *websocket.Dialer
}

type WebhookOptions struct {
	Identity  string
	Host      string
	Port      int
	Path      string
	Token     string
	APIConfig APIConfig
	Timeout   time.Duration
}

func webhookAddress(host string, port int) string {
	return fmt.Sprintf("%s:%d", host, port)
}

func normalizedTimeout(timeout time.Duration) time.Duration {
	if timeout <= 0 {
		return 300 * time.Second
	}
	return timeout
}
