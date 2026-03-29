package qq

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/WindowsSov8forUs/botgo-plus/dto"
	"github.com/WindowsSov8forUs/botgo-plus/errs"
	"github.com/WindowsSov8forUs/botgo-plus/sessions/manager"
	"github.com/WindowsSov8forUs/botgo-plus/websocket"
	"github.com/satori-protocol-go/satori-go/pkg/satori/logging"
	"github.com/satori-protocol-go/satori-go/pkg/satori/model/event"
	"github.com/satori-protocol-go/satori-go/pkg/satori/model/login"
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
		a.log(ctx, logging.LevelError, fmt.Sprintf("start websocket failed error=%v", err))
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
		err = a.normalizeWSError(withAppID(ctx, state.appID), state, err)
		if err != nil {
			a.log(ctx, logging.LevelError, fmt.Sprintf("qq websocket connection failed error=%v", err))
			a.log(
				ctx,
				logging.LevelWarn,
				fmt.Sprintf("websocket disconnected shard_id=%d shard_count=%d error=%v", target.ID, target.Count, err),
			)
			if manager.CanNotResume(err) {
				session.sessionID = ""
				session.sequence = 0
				session.hasSeq = false
			}
			if manager.CanNotIdentify(err) {
				a.log(ctx, logging.LevelError, fmt.Sprintf("websocket shard halted because identify is not allowed shard_id=%d shard_count=%d", target.ID, target.Count))
				return
			}
		}

		a.log(ctx, logging.LevelInfo, "reconnecting qq websocket gateway")
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
	if state == nil || state.token == nil {
		return errors.New("qq websocket requires token")
	}
	_ = state.token.InitToken(withAppID(ctx, state.appID))

	initialSession := dto.Session{
		ID:     strings.TrimSpace(session.sessionID),
		URL:    strings.TrimSpace(gatewayURL),
		Token:  *state.token,
		Intent: dto.Intent(a.wsIntents),
		Shards: dto.ShardConfig{
			ShardID:    target.ID,
			ShardCount: target.Count,
		},
	}
	if session.hasSeq && session.sequence > 0 {
		initialSession.LastSeq = uint32(session.sequence)
	}
	initialSession.PayloadParser = func(payload *dto.Payload) (bool, error) {
		return a.handleWSClientPayload(withAppID(ctx, state.appID), state, session, payload)
	}

	client := websocket.ClientImpl.New(initialSession)
	key := wsShardKey(state.appID, target.ID, target.Count)
	a.setWSClient(key, client)
	defer a.clearWSClient(key, client)

	if err := client.Connect(); err != nil {
		return err
	}
	if strings.TrimSpace(initialSession.ID) != "" {
		if err := client.Resume(); err != nil {
			return err
		}
	} else {
		if err := client.Identify(); err != nil {
			return err
		}
	}

	a.log(ctx, logging.LevelInfo, "qq websocket connected")
	a.publishLoginLifecycleByApp(state.appID, login.LoginStatusOnline, event.EventTypeLoginUpdated, true)

	stop := make(chan struct{})
	defer close(stop)
	go func() {
		select {
		case <-ctx.Done():
			client.Close()
		case <-stop:
		}
	}()

	err := client.Listening()
	current := client.Session()
	if current != nil {
		if strings.TrimSpace(current.ID) != "" {
			session.sessionID = strings.TrimSpace(current.ID)
		}
		if current.LastSeq > 0 {
			session.sequence = int64(current.LastSeq)
			session.hasSeq = true
		}
	}
	return err
}

func (a *Adapter) handleWSClientPayload(
	ctx context.Context,
	state *appState,
	session *wsShardSession,
	payload *dto.Payload,
) (bool, error) {
	if payload == nil {
		return true, nil
	}
	if sequence, ok := payloadSequence(payload); ok {
		session.sequence = sequence
		session.hasSeq = true
	}
	if payload.OPCode != dto.DispatchEvent {
		return true, nil
	}
	rawData := payloadDataFromEvent(payload)
	if payload.Type == "READY" {
		ready := &dto.WSReadyData{}
		if err := json.Unmarshal(rawData, ready); err == nil && strings.TrimSpace(ready.SessionID) != "" {
			session.sessionID = strings.TrimSpace(ready.SessionID)
		}
		return true, nil
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
		return true, nil
	}
	if evt != nil {
		a.logEventBySource(payload.Type, evt)
		a.pushEvent(evt)
	}
	return true, nil
}

func payloadDataFromEvent(payload *dto.Payload) json.RawMessage {
	if payload == nil {
		return nil
	}
	if len(bytes.TrimSpace(payload.RawMessage)) > 0 {
		envelope := &wsPayloadDataEnvelope{}
		if err := json.Unmarshal(payload.RawMessage, envelope); err == nil && len(envelope.Data) > 0 {
			return envelope.Data
		}
	}
	if payload.Data == nil {
		return nil
	}
	raw, err := json.Marshal(payload.Data)
	if err != nil {
		return nil
	}
	return raw
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

	startupInterval := manager.CalcInterval(1)
	if gatewayInfo != nil {
		limitPayload := *gatewayInfo
		limitPayload.Shards = uint32(len(targets))
		if err := manager.CheckSessionLimit(&limitPayload); err != nil {
			return "", nil, 0, err
		}
		startupInterval = manager.CalcInterval(gatewayInfo.SessionStartLimit.MaxConcurrency)
	}
	return gatewayURL, targets, startupInterval, nil
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

func (a *Adapter) normalizeWSError(_ context.Context, _ *appState, err error) error {
	if err == nil {
		return nil
	}
	typed := errs.Error(err)
	switch typed.Code() {
	case errs.CodeNeedReConnect:
		return errs.ErrNeedReConnect
	case errs.CodeConnCloseCantResume:
		return errs.ErrInvalidSession
	default:
		return err
	}
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
		logger.Log(context.Background(), logging.LevelWarn, fmt.Sprintf("unknown intent=%s", raw))
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

func (a *Adapter) setWSClient(key string, client websocket.WebSocket) {
	a.wsConnMu.Lock()
	if a.wsClients == nil {
		a.wsClients = map[string]websocket.WebSocket{}
	}
	a.wsClients[key] = client
	a.wsConnMu.Unlock()
}

func (a *Adapter) clearWSClient(key string, client websocket.WebSocket) {
	a.wsConnMu.Lock()
	if current, ok := a.wsClients[key]; ok && current == client {
		delete(a.wsClients, key)
	}
	a.wsConnMu.Unlock()
	if client != nil {
		client.Close()
	}
}

func (a *Adapter) closeAllWSConnections() {
	a.wsConnMu.Lock()
	clients := make([]websocket.WebSocket, 0, len(a.wsClients))
	for _, client := range a.wsClients {
		if client != nil {
			clients = append(clients, client)
		}
	}
	a.wsClients = map[string]websocket.WebSocket{}
	a.wsConnMu.Unlock()
	for _, client := range clients {
		client.Close()
	}
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
