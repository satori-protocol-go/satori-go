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
		gateway, targets, gatewayInfo, err := a.resolveWebSocketTargets(groupCtx, state)
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
		identify := newWSIdentifyGate(gatewayInfo)
		for _, target := range targets {
			group.Go(func() error { return a.runShardLoop(groupCtx, state, gateway, target, identify) })
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
	a.auditMu.Lock()
	a.audits = map[auditKey]*auditEntry{}
	a.auditMu.Unlock()
	a.closeAllWSConnections()
	a.mu.Lock()
	for _, info := range a.logins {
		info.Status = login.LoginStatusOffline
	}
	a.mu.Unlock()
	return nil
}

// All shards for one application share this gate, including fresh Identify on reconnect.
// Resume does not consume the session-creation quota. Separate processes must coordinate it.
type wsIdentifyGate struct {
	lock     chan struct{}
	next     time.Time
	interval time.Duration
	limit    *dto.SessionStartLimit
	resetAt  time.Time
}

func newWSIdentifyGate(info *dto.WebsocketAP) *wsIdentifyGate {
	gate := &wsIdentifyGate{lock: make(chan struct{}, 1), interval: 5 * time.Second}
	if info != nil {
		gate.configure(info.SessionStartLimit)
	}
	return gate
}

func (g *wsIdentifyGate) configure(limit dto.SessionStartLimit) {
	concurrency := time.Duration(limit.MaxConcurrency)
	if concurrency == 0 {
		concurrency = 1
	}
	g.interval = (5*time.Second + concurrency - 1) / concurrency
	g.limit = &limit
	g.resetAt = time.Time{}
	if limit.ResetAfter > 0 {
		g.resetAt = time.Now().Add(time.Duration(limit.ResetAfter) * time.Millisecond)
	}
}

func (g *wsIdentifyGate) connect(ctx context.Context, state *appState, connection websocket.WebSocket) error {
	select {
	case g.lock <- struct{}{}:
	case <-ctx.Done():
		return ctx.Err()
	}
	defer func() { <-g.lock }()
	if delay := time.Until(g.next); delay > 0 {
		timer := time.NewTimer(delay)
		defer timer.Stop()
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-timer.C:
		}
	}
	if g.limit != nil && !g.resetAt.IsZero() && !time.Now().Before(g.resetAt) {
		info, err := state.api.WS(withAppID(ctx, state.appID), nil, "")
		if err != nil {
			return err
		}
		if info == nil {
			return errors.New("QQ gateway returned no session limits")
		}
		g.configure(info.SessionStartLimit)
	}
	if g.limit != nil && g.limit.Remaining == 0 {
		return errs.ErrSessionLimit
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	// Wait before opening the socket, so queued shards do not time out before Identify.
	if err := connection.Connect(); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if g.limit != nil {
		g.limit.Remaining--
	}
	// Hold the gate through the write; slow connects/token retrieval cannot bunch up sends.
	defer func() { g.next = time.Now().Add(g.interval) }()
	return connection.Identify()
}

func (a *Adapter) runShardLoop(ctx context.Context, state *appState, gatewayURL string, target wsShardTarget, identify *wsIdentifyGate) error {
	session := &dto.Session{}
	for {
		if ctx.Err() != nil {
			return nil
		}
		err := a.runWebSocketSession(ctx, state, gatewayURL, target, session, identify)
		if ctx.Err() != nil {
			return nil
		}
		if statusErr := a.updateShardStatus(ctx, state, target.ID, false); statusErr != nil {
			return statusErr
		}
		if err != nil {
			a.log(ctx, logging.LevelWarn, fmt.Sprintf("QQ gateway shard %d for app %s ended with an error: %s", target.ID, logging.SafeText(state.appID), logging.ErrorText(err)))
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

func (a *Adapter) runWebSocketSession(ctx context.Context, state *appState, gatewayURL string, target wsShardTarget, session *dto.Session, identify *wsIdentifyGate) error {
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
	if initial.ID != "" {
		if err := connection.Connect(); err != nil {
			return err
		}
		if err := connection.Resume(); err != nil {
			return err
		}
	} else if err := identify.connect(ctx, state, connection); err != nil {
		return err
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
) (string, []wsShardTarget, *dto.WebsocketAP, error) {
	gatewayURL := strings.TrimSpace(a.wsGatewayURL)
	var gatewayInfo *dto.WebsocketAP
	if gatewayURL == "" {
		info, err := state.api.WS(withAppID(ctx, state.appID), nil, "")
		if err != nil {
			return "", nil, nil, err
		}
		if info == nil || strings.TrimSpace(info.URL) == "" {
			return "", nil, nil, errors.New("qq gateway url is empty")
		}
		if info.SessionStartLimit.Remaining == 0 {
			return "", nil, nil, errors.New("qq gateway session start limit reached")
		}
		gatewayURL = strings.TrimSpace(info.URL)
		gatewayInfo = info
	}

	targets := []wsShardTarget{}
	if a.wsShardCount > 0 {
		shardID := a.wsShardID
		if shardID >= a.wsShardCount {
			return "", nil, nil, errors.New("QQ shard ID is outside configured shard count")
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

	if gatewayInfo != nil {
		limitPayload := *gatewayInfo
		limitPayload.Shards = uint32(len(targets))
		if err := manager.CheckSessionLimit(&limitPayload); err != nil {
			return "", nil, nil, err
		}
	}
	return gatewayURL, targets, gatewayInfo, nil
}

func parseWSIntentNames(names []string) (int64, error) {
	// An omitted list uses the documented defaults; an explicit empty list does not.
	if names != nil && len(names) == 0 {
		return 0, errors.New("QQ WebSocket intents cannot be an empty list")
	}
	var intents dto.Intent
	for _, raw := range names {
		name := strings.ToUpper(strings.TrimSpace(raw))
		value, ok := wsIntentByName[name]
		if !ok {
			return 0, fmt.Errorf("unknown QQ WebSocket intent %q", raw)
		}
		intents |= value
	}
	return int64(intents), nil
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
	"OPEN_FORUM_EVENT":             1 << 18,
	"OPEN_FORUMS_EVENT":            1 << 18,
	"AUDIO_ACTION":                 dto.IntentAudio,
	"AUDIO_LIVE_MEMBER":            1 << 19,
	"AUDIO_OR_LIVE_CHANNEL_MEMBER": 1 << 19,
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
	description := fmt.Sprintf("QQ gateway shard %d is offline for app %s; the login is reconnecting.", shard, logging.SafeText(state.appID))
	if ready {
		description = fmt.Sprintf("QQ gateway shard %d is ready for app %s; waiting for the remaining shards.", shard, logging.SafeText(state.appID))
		if status == login.LoginStatusOnline {
			description = fmt.Sprintf("QQ gateway shard %d is ready for app %s; all required shards are online.", shard, logging.SafeText(state.appID))
		}
	}
	a.log(ctx, logging.LevelInfo, description)
	for _, evt := range events {
		if err := a.pushEvent(ctx, evt); err != nil {
			return err
		}
	}
	return nil
}
