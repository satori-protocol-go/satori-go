package qq

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/WindowsSov8forUs/botgo-plus/dto"
	"github.com/gorilla/websocket"
	"github.com/satori-protocol-go/satori-go/pkg/satori/logging"
	"github.com/satori-protocol-go/satori-go/pkg/satori/model/event"
	"github.com/satori-protocol-go/satori-go/pkg/satori/model/login"
)

var (
	errWSReconnect      = errors.New("qq gateway requires reconnect")
	errWSInvalidSession = errors.New("qq gateway invalid session")
)

type wsPayloadDataEnvelope struct {
	Data json.RawMessage `json:"d,omitempty"`
}

type wsShardTarget struct {
	ID    uint32
	Count uint32
}

type wsShardSession struct {
	sessionID string
	sequence  int64
	hasSeq    bool
}

func (a *Adapter) Block(ctx context.Context) error {
	if !a.wsEnabled {
		<-ctx.Done()
		return nil
	}
	state := a.primaryState()
	if state == nil || state.apiV1 == nil {
		return errors.New("qq websocket requires a valid app state")
	}

	gatewayURL, targets, startupInterval, err := a.resolveWebSocketTargets(ctx, state)
	if err != nil {
		a.log(ctx, logging.LevelError, fmt.Sprintf("启动 WebSocket 失败 error=%v", err))
		return err
	}

	var wg sync.WaitGroup
	for index, target := range targets {
		if index > 0 && startupInterval > 0 {
			timer := time.NewTimer(startupInterval)
			select {
			case <-ctx.Done():
				timer.Stop()
				wg.Wait()
				return nil
			case <-timer.C:
			}
		}
		wg.Add(1)
		go func(target wsShardTarget) {
			defer wg.Done()
			a.runShardLoop(ctx, state, gatewayURL, target)
		}(target)
	}

	<-ctx.Done()
	a.closeAllWSConnections()
	wg.Wait()
	return nil
}

func (a *Adapter) Cleanup(_ context.Context) error {
	a.closeAllWSConnections()
	for _, appID := range a.sortedAppIDs() {
		a.publishLoginLifecycleByApp(appID, login.LoginStatusOffline, event.EventTypeLoginRemoved, true)
	}
	return nil
}

func (a *Adapter) runShardLoop(ctx context.Context, state *appState, gatewayURL string, target wsShardTarget) {
	session := &wsShardSession{}
	for {
		if ctx.Err() != nil {
			return
		}

		err := a.runWebSocketSession(ctx, state, gatewayURL, target, session)
		if ctx.Err() != nil {
			return
		}
		if err != nil {
			a.log(ctx, logging.LevelError, fmt.Sprintf("QQ 开放平台连接出现错误 error=%v", err))
			a.log(
				ctx,
				logging.LevelWarn,
				fmt.Sprintf("websocket disconnected shard_id=%d shard_count=%d error=%v", target.ID, target.Count, err),
			)
		}

		a.log(ctx, logging.LevelInfo, "正在尝试重新连接 QQ 开放平台...")
		a.publishLoginLifecycleByApp(state.appID, login.LoginStatusReconnect, event.EventTypeLoginUpdated, true)

		timer := time.NewTimer(a.wsReconnect)
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
		}
	}
}

func (a *Adapter) runWebSocketSession(
	ctx context.Context,
	state *appState,
	gatewayURL string,
	target wsShardTarget,
	session *wsShardSession,
) error {
	dialer := websocket.Dialer{
		HandshakeTimeout: a.wsHandshake,
		Proxy:            http.ProxyFromEnvironment,
	}
	conn, _, err := dialer.DialContext(withAppID(ctx, state.appID), gatewayURL, nil)
	if err != nil {
		return err
	}
	key := wsShardKey(state.appID, target.ID, target.Count)
	a.setWSConnection(key, conn)
	defer a.clearWSConnection(key, conn)

	hello, err := readWSHello(conn)
	if err != nil {
		return err
	}
	if hello.HeartbeatInterval <= 0 {
		hello.HeartbeatInterval = int((30 * time.Second) / time.Millisecond)
	}
	a.log(ctx, logging.LevelInfo, fmt.Sprintf("成功与 QQ 开放平台建立 WebSocket 连接 heartbeat_interval_ms=%d", hello.HeartbeatInterval))

	if err := a.authenticateWS(withAppID(ctx, state.appID), conn, state, target, session); err != nil {
		return err
	}
	a.log(ctx, logging.LevelInfo, "WebSocket 连接成功")
	a.log(ctx, logging.LevelInfo, "连接成功！")
	a.publishLoginLifecycleByApp(state.appID, login.LoginStatusOnline, event.EventTypeLoginUpdated, true)

	heartbeatCtx, cancelHeartbeat := context.WithCancel(ctx)
	defer cancelHeartbeat()
	go a.heartbeatWS(heartbeatCtx, conn, time.Duration(hello.HeartbeatInterval)*time.Millisecond, session)

	for {
		if ctx.Err() != nil {
			return nil
		}
		payload, rawData, err := readWSGatewayPayload(conn)
		if err != nil {
			return err
		}

		if sequence, ok := payloadSequence(payload); ok {
			session.sequence = sequence
			session.hasSeq = true
		}

		switch payload.OPCode {
		case dto.WSHeartbeatAck:
			continue
		case dto.WSReconnect:
			a.log(ctx, logging.LevelInfo, "正在尝试重新连接 QQ 开放平台...")
			return errWSReconnect
		case dto.WSInvalidSession:
			session.sessionID = ""
			session.sequence = 0
			session.hasSeq = false
			return errWSInvalidSession
		case dto.DispatchEvent:
			if payload.Type == "READY" {
				ready := &dto.WSReadyData{}
				if err := json.Unmarshal(rawData, ready); err == nil && strings.TrimSpace(ready.SessionID) != "" {
					session.sessionID = strings.TrimSpace(ready.SessionID)
				}
				continue
			}

			if strings.HasPrefix(string(payload.Type), "MESSAGE_AUDIT_") {
				a.captureAuditResult(rawData)
			}
			if payload.Type == dto.EventInteractionCreate {
				a.ackInteraction(withAppID(ctx, state.appID), state, rawData)
			}

			evt, convertErr := a.converter.Convert(withAppID(ctx, state.appID), payload.OPCode, payload.Type, rawData)
			if convertErr != nil {
				a.log(ctx, logging.LevelWarn, fmt.Sprintf("websocket event convert failed error=%v", convertErr))
				continue
			}
			if evt != nil {
				a.logEventBySource(payload.Type, evt)
				a.pushEvent(evt)
			}
		}
	}
}

func (a *Adapter) resolveWebSocketTargets(
	ctx context.Context,
	state *appState,
) (string, []wsShardTarget, time.Duration, error) {
	gatewayURL := strings.TrimSpace(a.wsGatewayURL)
	var gatewayInfo *dto.WebsocketAP
	if gatewayURL == "" {
		info, err := state.apiV1.WS(withAppID(ctx, state.appID), nil, "")
		if err != nil {
			return "", nil, 0, err
		}
		if info == nil || strings.TrimSpace(info.URL) == "" {
			return "", nil, 0, errors.New("qq gateway url is empty")
		}
		if info.SessionStartLimit.Remaining == 0 {
			return "", nil, 0, errors.New("qq gateway session start limit reached")
		}
		gatewayURL = strings.TrimSpace(info.URL)
		gatewayInfo = info
	}

	targets := []wsShardTarget{}
	if a.wsShardCount > 0 {
		shardID := a.wsShardID
		if shardID >= a.wsShardCount {
			shardID = 0
		}
		targets = append(targets, wsShardTarget{ID: shardID, Count: a.wsShardCount})
	} else {
		shards := uint32(1)
		if gatewayInfo != nil && gatewayInfo.Shards > 0 {
			shards = gatewayInfo.Shards
		}
		for i := uint32(0); i < shards; i++ {
			targets = append(targets, wsShardTarget{ID: i, Count: shards})
		}
	}
	if len(targets) == 0 {
		targets = append(targets, wsShardTarget{ID: 0, Count: 1})
	}

	startupInterval := time.Second
	if gatewayInfo != nil && gatewayInfo.SessionStartLimit.MaxConcurrency > 0 {
		startupInterval = time.Duration(gatewayInfo.SessionStartLimit.MaxConcurrency) * time.Second
	}
	return gatewayURL, targets, startupInterval, nil
}

func (a *Adapter) authenticateWS(
	ctx context.Context,
	conn *websocket.Conn,
	state *appState,
	target wsShardTarget,
	session *wsShardSession,
) error {
	tokenValue, err := a.currentAuthorizationToken(ctx, state.selfID)
	if err != nil {
		return err
	}

	if strings.TrimSpace(session.sessionID) != "" {
		resume := &dto.WSResumeData{
			Token:     tokenValue,
			SessionID: session.sessionID,
			Seq:       uint32(session.sequence),
		}
		if !session.hasSeq {
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
		Shard:   []uint32{target.ID, target.Count},
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
		session.sequence = sequence
		session.hasSeq = true
	}
	if response.OPCode == dto.WSInvalidSession {
		session.sessionID = ""
		session.sequence = 0
		session.hasSeq = false
		return errWSInvalidSession
	}
	if response.OPCode != dto.DispatchEvent || response.Type != "READY" {
		return fmt.Errorf("unexpected payload before ready: op=%d type=%s", response.OPCode, response.Type)
	}

	ready := &dto.WSReadyData{}
	if err := json.Unmarshal(rawData, ready); err == nil && strings.TrimSpace(ready.SessionID) != "" {
		session.sessionID = strings.TrimSpace(ready.SessionID)
	}
	return nil
}

func (a *Adapter) heartbeatWS(
	ctx context.Context,
	conn *websocket.Conn,
	interval time.Duration,
	session *wsShardSession,
) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if strings.TrimSpace(session.sessionID) == "" {
				continue
			}
			payload := map[string]any{
				"op": dto.WSHeartbeat,
				"d":  nil,
			}
			if session.hasSeq {
				payload["d"] = session.sequence
			}
			if err := a.writeWSJSON(conn, payload); err != nil {
				_ = conn.Close()
				return
			}
		}
	}
}

func (a *Adapter) currentAuthorizationToken(ctx context.Context, selfID string) (string, error) {
	state := a.stateByAppID(appIDFromContext(ctx))
	if state == nil && strings.TrimSpace(selfID) != "" {
		state, _ = a.resolveStateBySelfID(ctx, selfID)
	}
	if state == nil {
		state = a.stateFromContextOrEvent(ctx, "")
	}
	if state == nil {
		state = a.primaryState()
	}
	if state == nil || state.token == nil {
		return "", errors.New("qq token is not configured")
	}

	tokenValue := strings.TrimSpace(state.token.GetString())
	if isAuthorizationTokenValid(tokenValue) {
		return tokenValue, nil
	}
	if err := state.token.InitToken(ctx); err == nil {
		tokenValue = strings.TrimSpace(state.token.GetString())
		if isAuthorizationTokenValid(tokenValue) {
			return tokenValue, nil
		}
	}

	if direct := strings.TrimSpace(state.directToken); direct != "" {
		return "QQBot " + direct, nil
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

func parseWSIntentNames(names []string, logger logging.Logger) int64 {
	if logger == nil {
		logger = logging.NopLogger{}
	}
	var intents dto.Intent
	for _, raw := range names {
		name := strings.ToUpper(strings.TrimSpace(raw))
		if name == "" {
			continue
		}
		if value, ok := wsIntentByName[name]; ok {
			intents |= value
			continue
		}
		logger.Log(context.Background(), logging.LevelWarn, fmt.Sprintf("未知的 intent=%s", raw))
	}
	return int64(intents)
}

var wsIntentByName = map[string]dto.Intent{
	"GUILDS":                       dto.IntentGuilds,
	"GUILD_MEMBERS":                dto.IntentGuildMembers,
	"GUILD_MESSAGES":               dto.IntentGuildMessages,
	"GUILD_MESSAGE_REACTIONS":      dto.IntentGuildMessageReactions,
	"GUILD_MESSAGE_REACTION":       dto.IntentGuildMessageReactions,
	"DIRECT_MESSAGES":              dto.IntentDirectMessages,
	"DIRECT_MESSAGE":               dto.IntentDirectMessages,
	"GROUP_AND_C2C_EVENT":          dto.IntentGroupAndC2CEvent,
	"C2C_GROUP_AT_MESSAGES":        dto.IntentGroupAndC2CEvent,
	"USER_MESSAGES":                dto.IntentGroupAndC2CEvent,
	"INTERACTION":                  dto.IntentInteraction,
	"MESSAGE_AUDIT":                dto.IntentMessageAudit,
	"FORUM_EVENT":                  dto.IntentForumEvent,
	"FORUMS_EVENT":                 dto.IntentForumEvent,
	"OPEN_FORUM_EVENT":             dto.IntentForumEvent,
	"OPEN_FORUMS_EVENT":            dto.IntentForumEvent,
	"AUDIO_ACTION":                 dto.IntentAudioAction,
	"AUDIO_LIVE_MEMBER":            dto.IntentAudioAction,
	"AUDIO_OR_LIVE_CHANNEL_MEMBER": dto.IntentAudioAction,
	"AT_MESSAGES":                  dto.IntentPublicGuildMessages,
	"PUBLIC_GUILD_MESSAGES":        dto.IntentPublicGuildMessages,
}

func wsShardKey(appID string, shardID uint32, shardCount uint32) string {
	return fmt.Sprintf("%s:%d/%d", strings.TrimSpace(appID), shardID, shardCount)
}

func (a *Adapter) setWSConnection(key string, conn *websocket.Conn) {
	a.wsConnMu.Lock()
	a.wsConn = conn
	if a.wsConns == nil {
		a.wsConns = map[string]*websocket.Conn{}
	}
	a.wsConns[key] = conn
	a.wsConnMu.Unlock()
}

func (a *Adapter) clearWSConnection(key string, conn *websocket.Conn) {
	a.wsConnMu.Lock()
	if current, ok := a.wsConns[key]; ok && current == conn {
		delete(a.wsConns, key)
	}
	if a.wsConn == conn {
		a.wsConn = nil
	}
	a.wsConnMu.Unlock()
	if conn != nil {
		_ = conn.Close()
	}
}

func (a *Adapter) closeAllWSConnections() {
	a.wsConnMu.Lock()
	connections := make([]*websocket.Conn, 0, len(a.wsConns)+1)
	for _, conn := range a.wsConns {
		if conn != nil {
			connections = append(connections, conn)
		}
	}
	if a.wsConn != nil {
		connections = append(connections, a.wsConn)
	}
	a.wsConns = map[string]*websocket.Conn{}
	a.wsConn = nil
	a.wsConnMu.Unlock()
	for _, conn := range connections {
		_ = conn.Close()
	}
}

func (a *Adapter) closeWSConnection() {
	a.closeAllWSConnections()
}

func (a *Adapter) publishLoginLifecycleByApp(
	appID string,
	status login.LoginStatus,
	eventType event.EventType,
	remove bool,
) {
	appID = strings.TrimSpace(appID)
	a.mu.Lock()
	events := make([]*event.Event, 0, 4)
	filtered := make([]*login.Login, 0, len(a.logins))
	for _, item := range a.logins {
		if item == nil || item.User == nil {
			continue
		}
		owner := a.selfToApp[item.User.Id]
		if owner != appID {
			filtered = append(filtered, item)
			continue
		}
		item.Status = status
		events = append(events, &event.Event{
			Type:      eventType,
			Timestamp: time.Now().UnixMilli(),
			Login:     cloneLogin(item),
		})
		if !remove {
			filtered = append(filtered, item)
		}
	}
	if remove {
		a.logins = filtered
		for selfID, owner := range a.selfToApp {
			if owner == appID {
				delete(a.selfToApp, selfID)
			}
		}
	}
	a.mu.Unlock()
	for _, item := range events {
		a.pushEvent(item)
	}
}

func (a *Adapter) ackInteraction(ctx context.Context, state *appState, rawData json.RawMessage) {
	if state == nil || state.apiV1 == nil {
		return
	}
	interaction := &dto.Interaction{}
	if err := json.Unmarshal(rawData, interaction); err != nil {
		return
	}
	if strings.TrimSpace(interaction.ID) == "" {
		return
	}
	_ = state.apiV1.PutInteraction(ctx, interaction.ID, `{"code":0}`)
}
