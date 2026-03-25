package qq

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"runtime"
	"strings"
	"time"

	"github.com/WindowsSov8forUs/botgo-plus/dto"
	"github.com/gorilla/websocket"
)

var (
	errWSReconnect      = errors.New("qq gateway requires reconnect")
	errWSInvalidSession = errors.New("qq gateway invalid session")
)

type wsPayloadDataEnvelope struct {
	Data json.RawMessage `json:"d,omitempty"`
}

func (a *Adapter) Block(ctx context.Context) error {
	if !a.wsEnabled {
		<-ctx.Done()
		return nil
	}

	for {
		if ctx.Err() != nil {
			return nil
		}

		err := a.runWebSocketSession(ctx)
		if ctx.Err() != nil {
			return nil
		}
		if err != nil {
			log.Printf("[qq-adapter] websocket disconnected: %v", err)
		}

		timer := time.NewTimer(a.wsReconnect)
		select {
		case <-ctx.Done():
			timer.Stop()
			return nil
		case <-timer.C:
		}
	}
}

func (a *Adapter) Cleanup(_ context.Context) error {
	a.closeWSConnection()
	return nil
}

func (a *Adapter) runWebSocketSession(ctx context.Context) error {
	gatewayURL, shardID, shardCount, err := a.resolveWebSocketGateway(ctx)
	if err != nil {
		return err
	}

	dialer := websocket.Dialer{
		HandshakeTimeout: a.wsHandshake,
		Proxy:            http.ProxyFromEnvironment,
	}
	conn, _, err := dialer.DialContext(ctx, gatewayURL, nil)
	if err != nil {
		return err
	}
	a.setWSConnection(conn)
	defer a.clearWSConnection(conn)

	hello, err := readWSHello(conn)
	if err != nil {
		return err
	}
	if hello.HeartbeatInterval <= 0 {
		hello.HeartbeatInterval = int((30 * time.Second) / time.Millisecond)
	}

	if err := a.authenticateWS(ctx, conn, shardID, shardCount); err != nil {
		return err
	}

	heartbeatCtx, cancelHeartbeat := context.WithCancel(ctx)
	defer cancelHeartbeat()
	go a.heartbeatWS(heartbeatCtx, conn, time.Duration(hello.HeartbeatInterval)*time.Millisecond)

	for {
		if ctx.Err() != nil {
			return nil
		}
		payload, rawData, err := readWSGatewayPayload(conn)
		if err != nil {
			return err
		}

		if sequence, ok := payloadSequence(payload); ok {
			a.setWSSequence(sequence, true)
		}

		switch payload.OPCode {
		case dto.WSHeartbeatAck:
			continue
		case dto.WSReconnect:
			return errWSReconnect
		case dto.WSInvalidSession:
			a.clearWSSession()
			return errWSInvalidSession
		case dto.DispatchEvent:
			if payload.Type == "READY" {
				ready := &dto.WSReadyData{}
				if err := json.Unmarshal(rawData, ready); err == nil && strings.TrimSpace(ready.SessionID) != "" {
					a.setWSSession(ready.SessionID)
				}
				continue
			}

			evt, convertErr := a.converter.Convert(ctx, payload.OPCode, payload.Type, rawData)
			if convertErr != nil {
				log.Printf("[qq-adapter] websocket event convert failed: %v", convertErr)
				continue
			}
			if evt != nil {
				a.pushEvent(evt)
			}
		}
	}
}

func (a *Adapter) resolveWebSocketGateway(ctx context.Context) (string, uint32, uint32, error) {
	gatewayURL := strings.TrimSpace(a.wsGatewayURL)
	shardCount := a.wsShardCount
	if gatewayURL == "" {
		info, err := a.apiV1.WS(ctx, nil, "")
		if err != nil {
			return "", 0, 0, err
		}
		if info == nil || strings.TrimSpace(info.URL) == "" {
			return "", 0, 0, errors.New("qq gateway url is empty")
		}
		gatewayURL = strings.TrimSpace(info.URL)
		if shardCount == 0 {
			shardCount = info.Shards
		}
	}

	if shardCount == 0 {
		shardCount = 1
	}
	shardID := a.wsShardID
	if shardID >= shardCount {
		shardID = 0
	}
	return gatewayURL, shardID, shardCount, nil
}

func (a *Adapter) authenticateWS(
	ctx context.Context,
	conn *websocket.Conn,
	shardID uint32,
	shardCount uint32,
) error {
	tokenValue, err := a.currentAuthorizationToken(ctx)
	if err != nil {
		return err
	}

	sessionID, hasSession := a.currentWSSession()
	seq, hasSeq := a.currentWSSequence()

	if hasSession {
		resume := &dto.WSResumeData{
			Token:     tokenValue,
			SessionID: sessionID,
			Seq:       uint32(seq),
		}
		if !hasSeq {
			resume.Seq = 0
		}
		payload := &dto.Payload{
			PayloadBase: dto.PayloadBase{OPCode: dto.WSResume},
			Data:        resume,
		}
		return a.writeWSJSON(conn, payload)
	}

	identity := &dto.WSIdentityData{
		Token:   tokenValue,
		Intents: dto.Intent(a.wsIntents),
		Shard:   []uint32{shardID, shardCount},
	}
	identity.Properties.Os = runtime.GOOS
	identity.Properties.Browser = "satori-go"
	identity.Properties.Device = "satori-go"

	payload := &dto.Payload{
		PayloadBase: dto.PayloadBase{OPCode: dto.WSIdentity},
		Data:        identity,
	}
	if err := a.writeWSJSON(conn, payload); err != nil {
		return err
	}

	response, rawData, err := readWSGatewayPayload(conn)
	if err != nil {
		return err
	}
	if sequence, ok := payloadSequence(response); ok {
		a.setWSSequence(sequence, true)
	}
	if response.OPCode == dto.WSInvalidSession {
		a.clearWSSession()
		return errWSInvalidSession
	}
	if response.OPCode != dto.DispatchEvent || response.Type != "READY" {
		return fmt.Errorf("unexpected payload before ready: op=%d type=%s", response.OPCode, response.Type)
	}

	ready := &dto.WSReadyData{}
	if err := json.Unmarshal(rawData, ready); err == nil && strings.TrimSpace(ready.SessionID) != "" {
		a.setWSSession(ready.SessionID)
	}
	return nil
}

func (a *Adapter) heartbeatWS(ctx context.Context, conn *websocket.Conn, interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			_, hasSession := a.currentWSSession()
			if !hasSession {
				continue
			}
			seq, hasSeq := a.currentWSSequence()
			payload := map[string]any{
				"op": dto.WSHeartbeat,
				"d":  nil,
			}
			if hasSeq {
				payload["d"] = seq
			}
			if err := a.writeWSJSON(conn, payload); err != nil {
				_ = conn.Close()
				return
			}
		}
	}
}

func (a *Adapter) currentAuthorizationToken(ctx context.Context) (string, error) {
	if a.token == nil {
		return "", errors.New("qq token is not configured")
	}

	tokenValue := strings.TrimSpace(a.token.GetString())
	if isAuthorizationTokenValid(tokenValue) {
		return tokenValue, nil
	}
	if err := a.token.InitToken(ctx); err == nil {
		tokenValue = strings.TrimSpace(a.token.GetString())
		if isAuthorizationTokenValid(tokenValue) {
			return tokenValue, nil
		}
	}

	if direct := strings.TrimSpace(a.cfg.Token); direct != "" {
		return "QQBot " + direct, nil
	}
	return "", errors.New("qq authorization token is empty")
}

func isAuthorizationTokenValid(tokenValue string) bool {
	tokenValue = strings.TrimSpace(tokenValue)
	if tokenValue == "" {
		return false
	}
	parts := strings.Fields(tokenValue)
	if len(parts) < 2 {
		return false
	}
	return strings.TrimSpace(parts[1]) != ""
}

func (a *Adapter) writeWSJSON(conn *websocket.Conn, payload any) error {
	a.wsWriteMu.Lock()
	defer a.wsWriteMu.Unlock()
	return conn.WriteJSON(payload)
}

func readWSHello(conn *websocket.Conn) (*dto.WSHelloData, error) {
	payload, rawData, err := readWSGatewayPayload(conn)
	if err != nil {
		return nil, err
	}
	if payload.OPCode != dto.WSHello {
		return nil, fmt.Errorf("unexpected payload before hello: op=%d type=%s", payload.OPCode, payload.Type)
	}
	hello := &dto.WSHelloData{}
	if err := json.Unmarshal(rawData, hello); err != nil {
		return nil, err
	}
	return hello, nil
}

func readWSGatewayPayload(conn *websocket.Conn) (*dto.Payload, json.RawMessage, error) {
	_, message, err := conn.ReadMessage()
	if err != nil {
		return nil, nil, err
	}
	decoder := json.NewDecoder(bytes.NewReader(message))
	decoder.UseNumber()
	payload := &dto.Payload{}
	if err := decoder.Decode(payload); err != nil {
		return nil, nil, err
	}
	envelope := &wsPayloadDataEnvelope{}
	if err := json.Unmarshal(message, envelope); err != nil {
		return nil, nil, err
	}
	return payload, envelope.Data, nil
}

func payloadSequence(payload *dto.Payload) (int64, bool) {
	if payload == nil {
		return 0, false
	}
	if payload.S > 0 {
		return payload.S, true
	}
	if payload.Seq > 0 {
		return int64(payload.Seq), true
	}
	return 0, false
}

func parseWSIntentNames(names []string) int64 {
	var intents dto.Intent
	for _, raw := range names {
		name := strings.ToUpper(strings.TrimSpace(raw))
		if name == "" {
			continue
		}
		if value, ok := wsIntentByName[name]; ok {
			intents |= value
		}
	}
	return int64(intents)
}

var wsIntentByName = map[string]dto.Intent{
	"GUILDS":                  dto.IntentGuilds,
	"GUILD_MEMBERS":           dto.IntentGuildMembers,
	"GUILD_MESSAGES":          dto.IntentGuildMessages,
	"GUILD_MESSAGE_REACTIONS": dto.IntentGuildMessageReactions,
	"DIRECT_MESSAGES":         dto.IntentDirectMessages,
	"GROUP_AND_C2C_EVENT":     dto.IntentGroupAndC2CEvent,
	"INTERACTION":             dto.IntentInteraction,
	"MESSAGE_AUDIT":           dto.IntentMessageAudit,
	"FORUM_EVENT":             dto.IntentForumEvent,
	"AUDIO_ACTION":            dto.IntentAudioAction,
	"AT_MESSAGES":             dto.IntentPublicGuildMessages,
}

func (a *Adapter) setWSConnection(conn *websocket.Conn) {
	a.wsConnMu.Lock()
	a.wsConn = conn
	a.wsConnMu.Unlock()
}

func (a *Adapter) clearWSConnection(conn *websocket.Conn) {
	a.wsConnMu.Lock()
	current := a.wsConn
	if current == conn {
		a.wsConn = nil
	}
	a.wsConnMu.Unlock()
	if conn != nil {
		_ = conn.Close()
	}
}

func (a *Adapter) closeWSConnection() {
	a.wsConnMu.Lock()
	conn := a.wsConn
	a.wsConn = nil
	a.wsConnMu.Unlock()
	if conn != nil {
		_ = conn.Close()
	}
}

func (a *Adapter) setWSSession(sessionID string) {
	a.wsConnMu.Lock()
	a.wsSession = strings.TrimSpace(sessionID)
	a.wsConnMu.Unlock()
}

func (a *Adapter) clearWSSession() {
	a.wsConnMu.Lock()
	a.wsSession = ""
	a.wsSequence = 0
	a.wsHasSeq = false
	a.wsConnMu.Unlock()
}

func (a *Adapter) currentWSSession() (string, bool) {
	a.wsConnMu.RLock()
	defer a.wsConnMu.RUnlock()
	if strings.TrimSpace(a.wsSession) == "" {
		return "", false
	}
	return a.wsSession, true
}

func (a *Adapter) setWSSequence(sequence int64, ok bool) {
	a.wsConnMu.Lock()
	a.wsSequence = sequence
	a.wsHasSeq = ok
	a.wsConnMu.Unlock()
}

func (a *Adapter) currentWSSequence() (int64, bool) {
	a.wsConnMu.RLock()
	defer a.wsConnMu.RUnlock()
	return a.wsSequence, a.wsHasSeq
}
