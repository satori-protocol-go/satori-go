package qq

import (
	"context"
	"log"
	"strings"
	"time"

	qqcodec "github.com/satori-protocol-go/satori-go/pkg/satori/adapter/qq/codec"
	"github.com/satori-protocol-go/satori-go/pkg/satori/model/event"
	"github.com/satori-protocol-go/satori-go/pkg/satori/model/login"
	"github.com/satori-protocol-go/satori-go/pkg/satori/model/user"
)

func (a *Adapter) ensureLogins(ctx context.Context) error {
	a.mu.RLock()
	if len(a.logins) > 0 {
		a.mu.RUnlock()
		return nil
	}
	a.mu.RUnlock()

	if ctx == nil {
		ctx = context.Background()
	}
	me, err := a.apiV1.Me(ctx)
	if err != nil {
		return err
	}

	identity := qqcodec.UserFromDTO(me)
	identity.IsBot = true

	a.mu.Lock()
	defer a.mu.Unlock()
	if len(a.logins) > 0 {
		return nil
	}

	a.selfID = identity.Id
	a.logins = []*login.Login{
		{
			Platform: "qq",
			User:     copyUser(identity),
			Status:   login.LoginStatusOnline,
			Adapter:  a.adapterName,
			Features: copyStrings(a.qqFeatures),
		},
		{
			Platform: "qqguild",
			User:     copyUser(identity),
			Status:   login.LoginStatusOnline,
			Adapter:  a.adapterName,
			Features: copyStrings(a.qqGuildFeatures),
		},
	}
	return nil
}

func (a *Adapter) loginForEventType(ctx context.Context, eventType string) *login.Login {
	platform := platformByEventType(eventType)
	if err := a.ensureLogins(ctx); err != nil {
		fallbackID := a.selfID
		if fallbackID == "" {
			fallbackID = a.appID
		}
		features := a.qqGuildFeatures
		if platform == "qq" {
			features = a.qqFeatures
		}
		return &login.Login{
			Platform: platform,
			User: &user.User{
				Id:    fallbackID,
				IsBot: true,
			},
			Status:   login.LoginStatusOnline,
			Adapter:  a.adapterName,
			Features: copyStrings(features),
		}
	}

	a.mu.RLock()
	defer a.mu.RUnlock()
	for _, item := range a.logins {
		if item == nil {
			continue
		}
		if item.Platform == platform {
			return cloneLogin(item)
		}
	}
	return nil
}

func (a *Adapter) loginForPlatform(ctx context.Context, platform string) *login.Login {
	if strings.TrimSpace(platform) == "" {
		return nil
	}
	if err := a.ensureLogins(ctx); err == nil {
		a.mu.RLock()
		defer a.mu.RUnlock()
		for _, item := range a.logins {
			if item == nil {
				continue
			}
			if item.Platform == platform {
				return cloneLogin(item)
			}
		}
	}

	features := a.qqGuildFeatures
	if platform == "qq" {
		features = a.qqFeatures
	}
	return &login.Login{
		Platform: platform,
		User: &user.User{
			Id:    firstNonEmpty(a.selfID, a.appID),
			IsBot: true,
		},
		Status:   login.LoginStatusOnline,
		Adapter:  a.adapterName,
		Features: copyStrings(features),
	}
}

func (a *Adapter) findLogin(platform string, selfID string) *login.Login {
	a.mu.RLock()
	defer a.mu.RUnlock()
	for _, item := range a.logins {
		if item == nil || item.User == nil {
			continue
		}
		if item.Platform == platform && item.User.Id == selfID {
			return cloneLogin(item)
		}
	}
	return nil
}

func (a *Adapter) bootstrap(ctx context.Context) {
	if err := a.ensureLogins(ctx); err != nil {
		log.Printf("[qq-adapter] bootstrap login failed: %v", err)
		return
	}
	logins, err := a.GetLogins(ctx)
	if err != nil {
		return
	}
	for _, item := range logins {
		if item == nil {
			continue
		}
		a.pushEvent(&event.Event{
			Type:      event.EventTypeLoginAdded,
			Timestamp: time.Now().UnixMilli(),
			Login:     item,
		})
	}
}

func (a *Adapter) pushEvent(evt *event.Event) {
	if evt == nil {
		return
	}
	select {
	case a.eventCh <- evt:
	default:
	}
}
