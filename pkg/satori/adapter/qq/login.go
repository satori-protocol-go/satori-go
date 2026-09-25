package qq

import (
	"context"
	"errors"
	"strings"

	"github.com/WindowsSov8forUs/botgo-plus/dto"
	"github.com/satori-protocol-go/satori-go/pkg/satori/adapter/qq/convert"
	"github.com/satori-protocol-go/satori-go/pkg/satori/model/event"
	"github.com/satori-protocol-go/satori-go/pkg/satori/model/login"
	"github.com/satori-protocol-go/satori-go/pkg/satori/model/user"
	"github.com/satori-protocol-go/satori-go/pkg/satori/server"
)

func (a *Adapter) ensureLogins(ctx context.Context) error {
	if err := a.eventContext.Err(); err != nil {
		return err
	}
	a.mu.RLock()
	ready := len(a.logins) > 0
	a.mu.RUnlock()
	if ready {
		return nil
	}
	a.loginInitMu.Lock()
	defer a.loginInitMu.Unlock()
	a.mu.RLock()
	ready = len(a.logins) > 0
	a.mu.RUnlock()
	if ready {
		return nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	logins := make([]*login.Login, 0, 2*len(a.appStates))
	owners := map[string]string{}
	for _, id := range a.sortedAppIDs() {
		state := a.appStates[id]
		me, err := state.api.Me(withAppID(ctx, id))
		if err != nil {
			return qqActionError(err)
		}
		identity := convert.UserFromDTO(me)
		if identity == nil || identity.Id == "" {
			return errors.New("QQ account response has no identity")
		}
		if owners[identity.Id] != "" {
			return server.NewActionError(409, "QQ application account identities are ambiguous", nil)
		}
		owners[identity.Id] = id
		identity.IsBot = true
		status := login.LoginStatusOnline
		if a.wsEnabled {
			status = login.LoginStatusConnect
		}
		for _, platform := range []string{"qq", "qqguild"} {
			features := a.qqFeatures
			if platform == "qqguild" {
				features = a.qqGuildFeatures
			}
			logins = append(logins, &login.Login{Platform: platform, User: copyUser(identity), Status: status, Adapter: a.adapterName, Features: copyStrings(features)})
		}
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	for _, info := range logins {
		info.Sn = a.nextLoginSN
		a.nextLoginSN++
	}
	a.logins = logins
	a.selfToApp = owners
	for selfID, id := range owners {
		a.appStates[id].selfID = selfID
	}
	return nil
}

func (a *Adapter) loginForEventType(ctx context.Context, eventType string) *login.Login {
	return a.loginForPlatform(ctx, platformByEventType(eventType))
}

func (a *Adapter) loginForPlatform(ctx context.Context, platform string) *login.Login {
	if platform == "" {
		return nil
	}
	state := a.stateFromContextOrEvent(ctx, "")
	if state == nil || a.ensureLogins(ctx) != nil {
		return nil
	}
	return a.findLoginInState(platform, state.appID)
}

func (a *Adapter) findLoginInState(platform, appID string) *login.Login {
	a.mu.RLock()
	defer a.mu.RUnlock()
	for _, item := range a.logins {
		if item != nil && item.User != nil && item.Platform == platform && a.selfToApp[item.User.Id] == appID {
			return cloneLogin(item)
		}
	}
	return nil
}

func (a *Adapter) findLogin(platform, selfID string) *login.Login {
	a.mu.RLock()
	defer a.mu.RUnlock()
	for _, item := range a.logins {
		if item != nil && item.User != nil && item.Platform == platform && item.User.Id == selfID {
			return cloneLogin(item)
		}
	}
	return nil
}

func (a *Adapter) pushEvent(ctx context.Context, evt *event.Event) error {
	if evt == nil {
		return nil
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := a.eventContext.Err(); err != nil {
		return err
	}
	select {
	case a.eventCh <- evt:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	case <-a.eventContext.Done():
		return a.eventContext.Err()
	}
}

func firstNonEmpty(items ...string) string {
	for _, item := range items {
		if strings.TrimSpace(item) != "" {
			return item
		}
	}
	return ""
}

func copyStrings(items []string) []string { return append([]string(nil), items...) }

func valueOrDefaultFeatures(values, defaults []string) []string {
	if values == nil {
		return copyStrings(defaults)
	}
	// Explicit declarations may include platform extensions, not just API names.
	return append([]string{}, values...)
}

func copyUser(item *user.User) *user.User {
	if item == nil {
		return nil
	}
	cloned := *item
	return &cloned
}

func cloneLogin(item *login.Login) *login.Login {
	if item == nil {
		return nil
	}
	return item.Clone()
}

func platformByEventType(eventType string) string {
	switch eventType {
	case string(dto.EventGroupMessageCreate), string(dto.EventGroupAtMessageCreate), string(dto.EventGroupMemberAdd), string(dto.EventGroupMemberRemove), string(dto.EventGroupJoinRequest), string(dto.EventGroupAddRobot), string(dto.EventGroupDelRobot),
		string(dto.EventGroupMsgReject), string(dto.EventGroupMsgReceive), string(dto.EventC2CMessageCreate),
		string(dto.EventC2CFriendAdd), string(dto.EventC2CFriendDel), "C2C_MSG_RECEIVE", "C2C_MSG_REJECT":
		return "qq"
	default:
		if dto.EventToIntent(dto.EventType(eventType)) != 0 {
			return "qqguild"
		}
		return ""
	}
}
