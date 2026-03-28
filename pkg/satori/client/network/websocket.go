package network

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	"github.com/satori-protocol-go/satori-go/pkg/satori/logging"
	"github.com/satori-protocol-go/satori-go/pkg/satori/model/event"
	"github.com/satori-protocol-go/satori-go/pkg/satori/model/login"
	"github.com/satori-protocol-go/satori-go/pkg/satori/model/operation"
	"github.com/satori-protocol-go/satori-go/pkg/satori/protocol"
)

type WS struct {
	base   *baseNetwork
	token  string
	wsBase string
	dialer *websocket.Dialer

	connMu sync.RWMutex
	conn   *websocket.Conn

	writeMu sync.Mutex
}

func NewWS(app AppBridge, options WebSocketOptions) *WS {
	dialer := copyDialer(options.Dialer)
	if options.HandshakeTimeout > 0 {
		dialer.HandshakeTimeout = options.HandshakeTimeout
	} else if dialer.HandshakeTimeout <= 0 {
		dialer.HandshakeTimeout = protocol.DefaultRequestTimeout
	}

	identity := strings.TrimSpace(options.Identity)
	if identity == "" {
		identity = "default"
	}

	network := &WS{
		token:  options.Token,
		wsBase: options.WSBase,
		dialer: dialer,
	}
	network.base = newBaseNetwork(app, options.APIConfig, fmt.Sprintf("satori/net/ws/%s#%p", identity, network), options.Logger)
	return network
}

func copyDialer(source *websocket.Dialer) *websocket.Dialer {
	if source == nil {
		defaultDialer := websocket.DefaultDialer
		if defaultDialer == nil {
			return &websocket.Dialer{}
		}
		copied := *defaultDialer
		return &copied
	}
	copied := *source
	return &copied
}

func (n *WS) ID() string {
	return n.base.ID()
}

func (n *WS) Run(ctx context.Context) error {
	if ctx == nil {
		ctx = context.Background()
	}

	for {
		if ctx.Err() != nil {
			n.base.app.MarkNetworkStatus(n.ID(), login.LoginStatusOffline, true)
			return nil
		}

		err := n.connectAndServe(ctx)
		if err == nil {
			continue
		}
		if errors.Is(err, context.Canceled) || ctx.Err() != nil {
			n.base.app.MarkNetworkStatus(n.ID(), login.LoginStatusOffline, true)
			return nil
		}

		n.base.Log(ctx, logging.LevelWarn, "websocket network disconnected",
			logging.Field{Key: "network_id", Value: n.ID()},
			logging.Field{Key: "error", Value: err},
		)
		n.base.app.MarkNetworkStatus(n.ID(), login.LoginStatusReconnect, false)

		select {
		case <-ctx.Done():
			n.base.app.MarkNetworkStatus(n.ID(), login.LoginStatusOffline, true)
			return nil
		case <-time.After(5 * time.Second):
		}
	}
}

func (n *WS) Close() error {
	n.base.MarkClosed()
	n.closeConnection()
	n.base.app.MarkNetworkStatus(n.ID(), login.LoginStatusOffline, true)
	return nil
}

func (n *WS) Alive() bool {
	return n.connection() != nil
}

func (n *WS) WaitForAvailable(ctx context.Context) error {
	return n.base.WaitAvailable(ctx)
}

func (n *WS) SetLogger(logger logging.Logger) {
	if n == nil || n.base == nil {
		return
	}
	n.base.SetLogger(logger)
}

func (n *WS) connectAndServe(ctx context.Context) error {
	wsEndpoint := joinURLPath(n.wsBase, "events")
	connection, response, err := n.dialer.DialContext(ctx, wsEndpoint, nil)
	if err != nil {
		if response != nil {
			_ = response.Body.Close()
		}
		return err
	}
	n.setConnection(connection)
	defer n.closeConnection()
	n.base.MarkAvailable()

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
	identify := operation.IdentifyBody{Token: n.token}
	if sequence := n.base.Sequence(); sequence > -1 {
		identify.Sn = sequence
	}
	if err := n.sendJSON(map[string]any{
		"op":   operation.OpcodeIdentify,
		"body": identify,
	}); err != nil {
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
	if err := decodeJSON(payload, &frame); err != nil {
		return err
	}
	if frame.Op != operation.OpcodeReady {
		return errors.New("unexpected websocket frame before ready")
	}

	var ready operation.ReadyBody
	if err := decodeJSON(frame.Body, &ready); err != nil {
		return err
	}

	n.base.SetProxyURLs(ready.ProxyUrls)
	n.base.app.SyncLogins(n.ID(), n.base.Config(), ready.ProxyUrls, ready.Logins)
	if len(ready.Logins) == 0 {
		n.base.Log(context.Background(), logging.LevelWarn, "no login available for websocket",
			logging.Field{Key: "network_id", Value: n.ID()},
		)
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
		if err := decodeJSON(payload, &frame); err != nil {
			continue
		}

		switch frame.Op {
		case operation.OpcodeEvent:
			var evt event.Event
			if err := decodeJSON(frame.Body, &evt); err != nil {
				n.base.Log(context.Background(), logging.LevelWarn, "failed to parse event payload",
					logging.Field{Key: "network_id", Value: n.ID()},
					logging.Field{Key: "error", Value: err},
				)
				continue
			}
			n.base.SetSequence(evt.Sn)
			// Keep receive loop responsive even with slow callbacks.
			go n.base.app.PostEvent(n.ID(), &evt)

		case operation.OpcodeMeta:
			var metaPayload operation.MetaBody
			if err := decodeJSON(frame.Body, &metaPayload); err != nil {
				continue
			}
			n.base.SetProxyURLs(metaPayload.ProxyUrls)
			n.base.app.SyncLogins(n.ID(), n.base.Config(), metaPayload.ProxyUrls, nil)

		case operation.OpcodePong:
			continue

		default:
			if frame.Op > operation.OpcodeMeta {
				n.base.Log(context.Background(), logging.LevelWarn, "received unknown opcode",
					logging.Field{Key: "network_id", Value: n.ID()},
					logging.Field{Key: "opcode", Value: frame.Op},
				)
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
		return errors.New("websocket connection is not established")
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

func joinURLPath(base string, suffix string) string {
	base = strings.TrimSuffix(strings.TrimSpace(base), "/")
	suffix = strings.TrimPrefix(strings.TrimSpace(suffix), "/")
	if base == "" {
		return "/" + suffix
	}
	if suffix == "" {
		return base
	}
	return base + "/" + suffix
}

var _ Runner = (*WS)(nil)
var _ Availability = (*WS)(nil)
var _ LoggerSetter = (*WS)(nil)
