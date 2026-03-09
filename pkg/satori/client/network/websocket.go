package network

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	"github.com/satori-protocol-go/satori-go/pkg/satori/model/event"
	"github.com/satori-protocol-go/satori-go/pkg/satori/model/login"
	"github.com/satori-protocol-go/satori-go/pkg/satori/model/operation"
)

type WS struct {
	base   *base
	token  string
	wsBase string
	dialer *websocket.Dialer

	connMu sync.RWMutex
	conn   *websocket.Conn

	writeMu sync.Mutex
}

func NewWS(app AppBridge, options WebSocketOptions) *WS {
	dialer := options.Dialer
	if dialer == nil {
		dialer = websocket.DefaultDialer
	}
	identity := options.Identity
	if identity == "" {
		identity = fmt.Sprintf("ws@%p", &options)
	}
	return &WS{
		base:   newBase(app, options.APIConfig, "satori/net/ws/"+identity),
		token:  options.Token,
		wsBase: options.WSBase,
		dialer: dialer,
	}
}

func (n *WS) ID() string {
	return n.base.ID()
}

func (n *WS) Run(ctx context.Context) error {
	for {
		if ctx.Err() != nil {
			n.base.app.MarkNetworkStatus(n.ID(), login.LoginStatusOffline, true)
			return nil
		}

		err := n.connectAndServe(ctx)
		if err == nil {
			continue
		}
		if ctx.Err() != nil || errors.Is(err, context.Canceled) {
			n.base.app.MarkNetworkStatus(n.ID(), login.LoginStatusOffline, true)
			return nil
		}

		log.Printf("[satori-client] websocket network %s disconnected: %v", n.ID(), err)
		n.base.app.MarkNetworkStatus(n.ID(), login.LoginStatusReconnect, false)

		select {
		case <-ctx.Done():
			n.base.app.MarkNetworkStatus(n.ID(), login.LoginStatusOffline, true)
			return nil
		case <-time.After(5 * time.Second):
		}
	}
}

func (n *WS) connectAndServe(ctx context.Context) error {
	wsEndpoint := stringsJoinPath(n.wsBase, "events")
	connection, response, err := n.dialer.DialContext(ctx, wsEndpoint, nil)
	if err != nil {
		if response != nil {
			_ = response.Body.Close()
		}
		return err
	}
	n.setConnection(connection)
	defer n.closeConnection()

	if err := n.authenticate(connection); err != nil {
		return err
	}

	recvErr := make(chan error, 1)
	heartbeatCtx, cancelHeartbeat := context.WithCancel(ctx)
	defer cancelHeartbeat()

	go func() {
		recvErr <- n.receiveLoop(connection)
	}()
	go n.heartbeatLoop(heartbeatCtx)

	select {
	case <-ctx.Done():
		return ctx.Err()
	case err := <-recvErr:
		return err
	}
}

func (n *WS) authenticate(connection *websocket.Conn) error {
	body := map[string]any{"token": n.token}
	if sequence := n.base.Sequence(); sequence > -1 {
		body["sn"] = sequence
	}
	if err := n.sendJSON(map[string]any{"op": operation.OpcodeIdentify, "body": body}); err != nil {
		return err
	}

	_ = connection.SetReadDeadline(time.Now().Add(30 * time.Second))
	_, payload, err := connection.ReadMessage()
	_ = connection.SetReadDeadline(time.Time{})
	if err != nil {
		return err
	}

	var frame struct {
		Op   operation.Opcode `json:"op"`
		Body json.RawMessage  `json:"body"`
	}
	if err := json.Unmarshal(payload, &frame); err != nil {
		return err
	}
	if frame.Op != operation.OpcodeReady {
		return errors.New("unexpected websocket ready payload")
	}

	var ready operation.ReadyBody
	if err := json.Unmarshal(frame.Body, &ready); err != nil {
		return err
	}

	n.base.SetProxyURLs(ready.ProxyUrls)
	n.base.app.SyncLogins(n.ID(), n.base.Config(), ready.ProxyUrls, ready.Logins)
	if len(ready.Logins) == 0 {
		log.Printf("[satori-client] no account available for websocket %s", n.ID())
	}
	return nil
}

func (n *WS) receiveLoop(connection *websocket.Conn) error {
	for {
		_, payload, err := connection.ReadMessage()
		if err != nil {
			return err
		}

		var frame struct {
			Op   operation.Opcode `json:"op"`
			Body json.RawMessage  `json:"body"`
		}
		if err := json.Unmarshal(payload, &frame); err != nil {
			continue
		}

		switch frame.Op {
		case operation.OpcodeEvent:
			var evt event.Event
			if err := json.Unmarshal(frame.Body, &evt); err != nil {
				log.Printf("[satori-client] failed to parse event payload: %v", err)
				continue
			}
			n.base.SetSequence(evt.Sn)
			n.base.app.PostEvent(n.ID(), &evt)
		case operation.OpcodeMeta:
			var payload operation.MetaBody
			if err := json.Unmarshal(frame.Body, &payload); err != nil {
				continue
			}
			n.base.SetProxyURLs(payload.ProxyUrls)
			n.base.app.SyncLogins(n.ID(), n.base.Config(), payload.ProxyUrls, nil)
		default:
			if frame.Op > operation.OpcodeMeta {
				log.Printf("[satori-client] received unknown opcode: %d", frame.Op)
			}
		}
	}
}

func (n *WS) heartbeatLoop(ctx context.Context) {
	ticker := time.NewTicker(9 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := n.sendJSON(map[string]any{"op": operation.OpcodePing}); err != nil {
				return
			}
		}
	}
}

func (n *WS) sendJSON(payload any) error {
	n.writeMu.Lock()
	defer n.writeMu.Unlock()

	connection := n.connection()
	if connection == nil {
		return errors.New("connection is not established")
	}
	return connection.WriteJSON(payload)
}

func (n *WS) connection() *websocket.Conn {
	n.connMu.RLock()
	defer n.connMu.RUnlock()
	return n.conn
}

func (n *WS) setConnection(connection *websocket.Conn) {
	n.connMu.Lock()
	n.conn = connection
	n.connMu.Unlock()
}

func (n *WS) closeConnection() {
	n.connMu.Lock()
	connection := n.conn
	n.conn = nil
	n.connMu.Unlock()
	if connection != nil {
		_ = connection.Close()
	}
}

func (n *WS) Close() error {
	n.base.MarkClosed()
	n.closeConnection()
	n.base.app.MarkNetworkStatus(n.ID(), login.LoginStatusOffline, true)
	return nil
}

func stringsJoinPath(base string, path string) string {
	base = strings.TrimSuffix(strings.TrimSpace(base), "/")
	path = strings.TrimPrefix(strings.TrimSpace(path), "/")
	if base == "" {
		return "/" + path
	}
	if path == "" {
		return base
	}
	return base + "/" + path
}

var _ Runner = (*WS)(nil)
