package server

import (
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gorilla/websocket"
	"github.com/satori-protocol-go/satori-go/pkg/satori/logging"
	"github.com/satori-protocol-go/satori-go/pkg/satori/model/operation"
)

var websocketConnectionCounter atomic.Uint64

type websocketConnection struct {
	connection  *websocket.Conn
	closeSignal chan struct{}
	closeOnce   sync.Once
	writeMu     sync.Mutex

	id         string
	remoteAddr string

	logFn func(level LogLevel, v ...any)

	stateMu              sync.RWMutex
	closeReason          string
	closeErr             error
	lastHeartbeatAt      time.Time
	lastHeartbeatLatency time.Duration
}

func newWebsocketConnection(
	connection *websocket.Conn,
	remoteAddr string,
	logFn func(level LogLevel, v ...any),
) *websocketConnection {
	connectionID := strconv.FormatUint(websocketConnectionCounter.Add(1), 10)
	return &websocketConnection{
		connection:  connection,
		closeSignal: make(chan struct{}),
		id:          connectionID,
		remoteAddr:  remoteAddr,
		logFn:       logFn,
	}
}

func (c *websocketConnection) ID() string {
	return c.id
}

func (c *websocketConnection) RemoteAddr() string {
	return c.remoteAddr
}

func (c *websocketConnection) Alive() bool {
	select {
	case <-c.closeSignal:
		return false
	default:
		return true
	}
}

func (c *websocketConnection) Closed() <-chan struct{} {
	return c.closeSignal
}

func (c *websocketConnection) WaitClosed() {
	<-c.closeSignal
}

func (c *websocketConnection) CloseWith(code int, reason string) error {
	if reason != "" {
		c.setCloseInfo(reason, nil)
	} else {
		c.setCloseInfo(fmt.Sprintf("closed with code %d", code), nil)
	}

	c.writeMu.Lock()
	writeErr := c.connection.WriteControl(
		websocket.CloseMessage,
		websocket.FormatCloseMessage(code, reason),
		time.Now().Add(time.Second),
	)
	c.writeMu.Unlock()
	if writeErr != nil {
		c.setCloseInfo("failed to send the close frame", writeErr)
	}
	return c.Close()
}

func (c *websocketConnection) Close() error {
	var closeErr error
	c.closeOnce.Do(func() {
		c.setCloseInfo("connection closed", nil)
		close(c.closeSignal)
		closeErr = c.connection.Close()
		if closeErr != nil {
			c.setCloseInfo("failed to close the connection", closeErr)
		}
	})
	return closeErr
}

func (c *websocketConnection) Send(payload any) error {
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	if err := c.connection.SetWriteDeadline(time.Now().Add(10 * time.Second)); err != nil {
		return err
	}
	if err := c.connection.WriteJSON(payload); err != nil {
		c.setCloseInfo("failed to write a frame", err)
		return err
	}
	return nil
}

func (c *websocketConnection) Heartbeat(timeout time.Duration) {
	if timeout <= 0 {
		timeout = 12 * time.Second
	}
	for {
		select {
		case <-c.closeSignal:
			return
		default:
		}

		start := time.Now()
		c.connection.SetReadDeadline(time.Now().Add(timeout))
		_, payload, err := c.connection.ReadMessage()
		if err != nil {
			if isTimeoutError(err) {
				c.setCloseInfo("heartbeat timed out", err)
				c.log(LogLevelWarn, fmt.Sprintf(
					"WebSocket client %s at %s timed out while waiting for a heartbeat: %s",
					c.id,
					logging.SafeText(c.remoteAddr),
					logging.ErrorText(err),
				))
			} else {
				var closeErr *websocket.CloseError
				if errors.As(err, &closeErr) {
					c.setCloseInfo(
						fmt.Sprintf("peer closed the connection with code %d", closeErr.Code),
						err,
					)
				} else {
					c.setCloseInfo("failed to read a frame", err)
				}
			}
			_ = c.Close()
			return
		}

		var message struct {
			Op operation.Opcode `json:"op"`
		}
		if err := json.Unmarshal(payload, &message); err != nil {
			continue
		}
		if message.Op != operation.OpcodePing {
			continue
		}

		latency := time.Since(start)
		c.setHeartbeat(latency)
		if err := c.Send(map[string]any{"op": operation.OpcodePong}); err != nil {
			c.log(LogLevelWarn, fmt.Sprintf(
				"Failed to send PONG to WebSocket client %s at %s: %s",
				c.id,
				logging.SafeText(c.remoteAddr),
				logging.ErrorText(err),
			))
			_ = c.Close()
			return
		}
		c.log(LogLevelDebug, fmt.Sprintf(
			"Sent PONG to WebSocket client %s at %s after waiting %d ms for input.",
			c.id,
			logging.SafeText(c.remoteAddr),
			latency.Milliseconds(),
		))
	}
}

func (c *websocketConnection) CloseInfo() (string, error) {
	c.stateMu.RLock()
	defer c.stateMu.RUnlock()
	return c.closeReason, c.closeErr
}

func (c *websocketConnection) LastHeartbeat() (time.Time, time.Duration) {
	c.stateMu.RLock()
	defer c.stateMu.RUnlock()
	return c.lastHeartbeatAt, c.lastHeartbeatLatency
}

func (c *websocketConnection) setCloseInfo(reason string, err error) {
	c.stateMu.Lock()
	if c.closeReason == "" && reason != "" {
		c.closeReason = reason
	}
	if c.closeErr == nil && err != nil {
		c.closeErr = err
	}
	c.stateMu.Unlock()
}

func (c *websocketConnection) setHeartbeat(latency time.Duration) {
	c.stateMu.Lock()
	c.lastHeartbeatAt = time.Now()
	c.lastHeartbeatLatency = latency
	c.stateMu.Unlock()
}

func (c *websocketConnection) log(level LogLevel, v ...any) {
	if c.logFn == nil {
		return
	}
	c.logFn(level, v...)
}

func isTimeoutError(err error) bool {
	var netErr net.Error
	return errors.As(err, &netErr) && netErr.Timeout()
}
