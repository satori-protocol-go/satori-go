package server

import (
	"encoding/json"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	"github.com/satori-protocol-go/satori-go/pkg/satori/model/operation"
)

type websocketConnection struct {
	connection  *websocket.Conn
	closeSignal chan struct{}
	closeOnce   sync.Once
	writeMu     sync.Mutex
}

func newWebsocketConnection(connection *websocket.Conn) *websocketConnection {
	return &websocketConnection{
		connection:  connection,
		closeSignal: make(chan struct{}),
	}
}

func (c *websocketConnection) Alive() bool {
	select {
	case <-c.closeSignal:
		return false
	default:
		return true
	}
}

func (c *websocketConnection) CloseWith(code int, reason string) error {
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	_ = c.connection.WriteControl(
		websocket.CloseMessage,
		websocket.FormatCloseMessage(code, reason),
		time.Now().Add(time.Second),
	)
	return c.Close()
}

func (c *websocketConnection) Close() error {
	var closeErr error
	c.closeOnce.Do(func() {
		close(c.closeSignal)
		closeErr = c.connection.Close()
	})
	return closeErr
}

func (c *websocketConnection) Send(payload any) error {
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	return c.connection.WriteJSON(payload)
}

func (c *websocketConnection) Heartbeat(timeout time.Duration) {
	for {
		c.connection.SetReadDeadline(time.Now().Add(timeout))
		_, payload, err := c.connection.ReadMessage()
		if err != nil {
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

		if err := c.Send(map[string]any{"op": operation.OpcodePong}); err != nil {
			_ = c.Close()
			return
		}
	}
}
