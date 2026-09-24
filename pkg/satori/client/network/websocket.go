package network

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gorilla/websocket"
	"github.com/satori-protocol-go/satori-go/pkg/satori/logging"
	"github.com/satori-protocol-go/satori-go/pkg/satori/model/event"
	"github.com/satori-protocol-go/satori-go/pkg/satori/model/login"
	"github.com/satori-protocol-go/satori-go/pkg/satori/model/operation"
	"github.com/satori-protocol-go/satori-go/pkg/satori/protocol"
)

const eventQueueSize = 128
const pingInterval = 10 * time.Second

type wsFrame struct {
	Op   *operation.Opcode `json:"op"`
	Body json.RawMessage   `json:"body"`
}

type WS struct {
	base   *baseNetwork
	token  string
	wsBase string
	dialer *websocket.Dialer

	connMu    sync.RWMutex
	conn      *websocket.Conn
	runCancel context.CancelFunc

	writeMu          sync.Mutex
	heartbeatPending atomic.Bool
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
	ctx, cancel := context.WithCancel(ctx)
	n.connMu.Lock()
	if n.runCancel != nil {
		n.connMu.Unlock()
		cancel()
		return errors.New("websocket network is already running")
	}
	n.runCancel = cancel
	n.connMu.Unlock()
	defer func() { cancel(); n.connMu.Lock(); n.runCancel = nil; n.connMu.Unlock() }()
	select {
	case <-n.base.CloseSignal():
		return nil
	default:
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

		n.base.Log(ctx, logging.LevelWarn, fmt.Sprintf("websocket network disconnected network_id=%s error=%v", n.ID(), err))
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
	n.connMu.RLock()
	cancel := n.runCancel
	n.connMu.RUnlock()
	if cancel != nil {
		cancel()
	}
	n.closeConnection()
	n.base.app.MarkNetworkStatus(n.ID(), login.LoginStatusOffline, true)
	return nil
}

func (n *WS) Alive() bool {
	return n.base.Available() && n.connection() != nil
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
		if response != nil && response.Body != nil {
			_ = response.Body.Close()
		}
		return err
	}
	n.setConnection(connection)
	workCtx, cancel := context.WithCancel(ctx)
	closeOnCancel := context.AfterFunc(workCtx, func() { _ = connection.Close() })
	var workers sync.WaitGroup
	defer func() {
		cancel()
		closeOnCancel()
		n.closeConnection()
		workers.Wait()
		n.base.MarkUnavailable()
	}()
	n.heartbeatPending.Store(false)
	if err := n.authenticate(connection); err != nil {
		return err
	}
	if err := workCtx.Err(); err != nil {
		return err
	}
	n.base.MarkAvailable()

	frames := make(chan wsFrame, eventQueueSize)
	results := make(chan error, 2)
	workers.Add(2)
	go func() { defer workers.Done(); results <- n.receiveLoop(workCtx, connection, frames) }()
	go func() { defer workers.Done(); results <- n.dispatchFrames(workCtx, frames) }()
	ticker := time.NewTicker(pingInterval)
	defer ticker.Stop()
	for {
		select {
		case <-workCtx.Done():
			return workCtx.Err()
		case err := <-results:
			return err
		case <-ticker.C:
			if err := n.heartbeatTick(); err != nil {
				return err
			}
		}
	}
}

func (n *WS) authenticate(connection *websocket.Conn) error {
	identify := operation.IdentifyBody{Token: n.token}
	if sequence := n.base.Sequence(); sequence > -1 {
		identify.Sn = &sequence
	}
	if err := n.sendJSON(map[string]any{
		"op":   operation.OpcodeIdentify,
		"body": identify,
	}); err != nil {
		return err
	}

	_ = connection.SetReadDeadline(time.Now().Add(10 * time.Second))
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
	if err := n.base.app.SyncLogins(n.ID(), n.base.Config(), ready.ProxyUrls, ready.Logins); err != nil {
		return err
	}
	if len(ready.Logins) == 0 {
		n.base.Log(context.Background(), logging.LevelWarn, fmt.Sprintf("no login available for websocket network_id=%s", n.ID()))
	}
	return nil
}

func (n *WS) receiveLoop(ctx context.Context, connection *websocket.Conn, frames chan<- wsFrame) error {
	defer close(frames)
	for {
		_, payload, err := connection.ReadMessage()
		if err != nil {
			return err
		}
		var frame wsFrame
		if err := decodeJSON(payload, &frame); err != nil {
			return err
		}
		if frame.Op == nil {
			return errors.New("Satori frame has no opcode")
		}
		switch *frame.Op {
		case operation.OpcodePong:
			n.heartbeatPending.Store(false)
		case operation.OpcodeEvent, operation.OpcodeMeta:
			select {
			case <-ctx.Done():
				return ctx.Err()
			case frames <- frame:
			}
		default:
			n.base.Log(ctx, logging.LevelDebug, fmt.Sprintf("unhandled Satori opcode network_id=%s opcode=%d", n.ID(), *frame.Op))
		}
	}
}

// dispatchFrames serializes state changes and callbacks for this connection.
// Advancing sn records a completed local processing attempt, not durable delivery.
func (n *WS) dispatchFrames(ctx context.Context, frames <-chan wsFrame) error {
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case frame, open := <-frames:
			if !open {
				return nil
			}
			if err := ctx.Err(); err != nil {
				return err
			}
			switch *frame.Op {
			case operation.OpcodeEvent:
				var evt event.Event
				if err := decodeJSON(frame.Body, &evt); err != nil {
					return err
				}
				if evt.Login == nil || evt.Type == "" || evt.Sn < 0 {
					return errors.New("invalid Satori event envelope")
				}
				if err := n.base.app.PostEvent(n.ID(), &evt); err != nil {
					n.base.Log(ctx, logging.LevelError, fmt.Sprintf("event handling failed network_id=%s event_sn=%d error=%v", n.ID(), evt.Sn, err))
				}
				n.base.SetSequence(evt.Sn)
			case operation.OpcodeMeta:
				var metaPayload operation.MetaBody
				if err := decodeJSON(frame.Body, &metaPayload); err != nil {
					return err
				}
				n.base.SetProxyURLs(metaPayload.ProxyUrls)
				n.base.app.UpdateProxyURLs(n.ID(), metaPayload.ProxyUrls)
			}
		}
	}
}

func (n *WS) heartbeatTick() error {
	if n.heartbeatPending.Swap(true) {
		return errors.New("Satori PONG timeout")
	}
	return n.sendJSON(map[string]any{"op": operation.OpcodePing})
}

func (n *WS) sendJSON(payload any) error {
	n.writeMu.Lock()
	defer n.writeMu.Unlock()

	connection := n.connection()
	if connection == nil {
		return errors.New("websocket connection is not established")
	}
	if err := connection.SetWriteDeadline(time.Now().Add(10 * time.Second)); err != nil {
		return err
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
