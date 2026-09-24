package qq

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"golang.org/x/sync/errgroup"
	"strings"
	"time"

	"github.com/WindowsSov8forUs/botgo-plus/dto"
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

func (a *Adapter) Block(ctx context.Context) error {
	if err := a.Prepare(ctx); err != nil {
		return err
	}
	runCtx, cancel := context.WithCancel(ctx)
	stop := context.AfterFunc(a.eventContext, cancel)
	defer stop()
	defer cancel()
	if !a.wsEnabled {
		<-runCtx.Done()
		return nil
	}
	group, groupCtx := errgroup.WithContext(runCtx)
	defer a.closeAllWSConnections()
	for _, id := range a.sortedAppIDs() {
		state := a.appStates[id]
		gateway, targets, interval, err := a.resolveWebSocketTargets(groupCtx, state)
		if err != nil {
			cancel()
			a.closeAllWSConnections()
			_ = group.Wait()
			return err
		}
		a.mu.Lock()
		state.expectedShards = len(targets)
		state.readyShards = map[uint32]bool{}
		a.mu.Unlock()
		for index, target := range targets {
			if index > 0 && interval > 0 {
				timer := time.NewTimer(interval)
				select {
				case <-groupCtx.Done():
					timer.Stop()
					cancel()
					a.closeAllWSConnections()
					return group.Wait()
				case <-timer.C:
				}
			}
			group.Go(func() error { return a.runShardLoop(groupCtx, state, gateway, target) })
		}
	}
	err := group.Wait()
	if runCtx.Err() != nil {
		return nil
	}
	return err
}

func (a *Adapter) Cleanup(_ context.Context) error {
	a.cancelEvents()
	a.closeAllWSConnections()
	a.mu.Lock()
	for _, info := range a.logins {
		info.Status = login.LoginStatusOffline
	}
	a.mu.Unlock()
	return nil
}

func (a *Adapter) runShardLoop(ctx context.Context, state *appState, gatewayURL string, target wsShardTarget) error {
	session := &dto.Session{}
	for {
		if ctx.Err() != nil {
			return nil
		}
		err := a.runWebSocketSession(ctx, state, gatewayURL, target, session)
		if ctx.Err() != nil {
			return nil
		}
		if statusErr := a.updateShardStatus(ctx, state, target.ID, false); statusErr != nil {
			return statusErr
		}
		if err != nil {
			a.log(ctx, logging.LevelWarn, fmt.Sprintf("QQ gateway disconnected app_id=%s shard_id=%d error=%v", state.appID, target.ID, err))
			if manager.CanNotResume(err) {
				session.ID = ""
				session.LastSeq = 0
			}
			if manager.CanNotIdentify(err) {
				return err
			}
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

func (a *Adapter) runWebSocketSession(ctx context.Context, state *appState, gatewayURL string, target wsShardTarget, session *dto.Session) error {
	initial := *session
	initial.URL = gatewayURL
	initial.AppID = state.appID
	initial.TokenSource = state.token
	initial.Intent = dto.Intent(a.wsIntents)
	initial.Shards = dto.ShardConfig{ShardID: target.ID, ShardCount: target.Count}
	initial.EventHandler = func(callbackCtx context.Context, payload *dto.WSPayload) error {
		if payload.Type == "READY" || payload.Type == "RESUMED" {
			return a.updateShardStatus(callbackCtx, state, target.ID, true)
		}
		return a.acceptPayload(withAppID(callbackCtx, state.appID), state, payload)
	}
	connection := websocket.ClientImpl.New(initial)
	key := wsShardKey(state.appID, target.ID, target.Count)
	a.setWSClient(key, connection)
	defer a.clearWSClient(key, connection)
	stop := context.AfterFunc(ctx, connection.Close)
	defer stop()
	if err := connection.Connect(); err != nil {
		return err
	}
	if initial.ID != "" {
		if err := connection.Resume(); err != nil {
			return err
		}
	} else {
		if err := connection.Identify(); err != nil {
			return err
		}
	}
	err := connection.Listening()
	*session = *connection.Session()
	return err
}

func payloadDataFromEvent(payload *dto.WSPayload) json.RawMessage {
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
		info, err := state.api.WS(withAppID(ctx, state.appID), nil, "")
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
			return "", nil, 0, errors.New("QQ shard ID is outside configured shard count")
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
	"GROUP_AND_C2C_EVENT":          dto.IntentGroupMessages,
	"C2C_GROUP_AT_MESSAGES":        dto.IntentGroupMessages,
	"USER_MESSAGES":                dto.IntentGroupMessages,
	"INTERACTION":                  dto.IntentInteraction,
	"MESSAGE_AUDIT":                dto.IntentAudit,
	"FORUM_EVENT":                  dto.IntentForum,
	"FORUMS_EVENT":                 dto.IntentForum,
	"OPEN_FORUM_EVENT":             dto.IntentForum,
	"OPEN_FORUMS_EVENT":            dto.IntentForum,
	"AUDIO_ACTION":                 dto.IntentAudio,
	"AUDIO_LIVE_MEMBER":            dto.IntentAudio,
	"AUDIO_OR_LIVE_CHANNEL_MEMBER": dto.IntentAudio,
	"AT_MESSAGES":                  dto.IntentGuildAtMessage,
	"PUBLIC_GUILD_MESSAGES":        dto.IntentGuildAtMessage,
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

// An application's login is fully online once each configured shard is ready.
func (a *Adapter) updateShardStatus(ctx context.Context, state *appState, shard uint32, ready bool) error {
	a.mu.Lock()
	if state.readyShards == nil {
		state.readyShards = map[uint32]bool{}
	}
	if ready {
		state.readyShards[shard] = true
	} else {
		delete(state.readyShards, shard)
	}
	status := login.LoginStatusReconnect
	if len(state.readyShards) == state.expectedShards {
		status = login.LoginStatusOnline
	}
	events := make([]*event.Event, 0, 2)
	for _, info := range a.logins {
		if info.User == nil || a.selfToApp[info.User.Id] != state.appID || info.Status == status {
			continue
		}
		info.Status = status
		events = append(events, &event.Event{Type: event.EventTypeLoginUpdated, Timestamp: time.Now().UnixMilli(), Login: cloneLogin(info)})
	}
	a.mu.Unlock()
	for _, evt := range events {
		if err := a.pushEvent(ctx, evt); err != nil {
			return err
		}
	}
	return nil
}
