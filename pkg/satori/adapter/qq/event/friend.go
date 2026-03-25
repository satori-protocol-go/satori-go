package event

import (
	satorievent "github.com/satori-protocol-go/satori-go/pkg/satori/model/event"
	"github.com/satori-protocol-go/satori-go/pkg/satori/model/login"
	"github.com/satori-protocol-go/satori-go/pkg/satori/model/user"
)

func (c *Converter) makeFriendEvent(
	loginValue *login.Login,
	data map[string]any,
	eventType satorievent.EventType,
) *satorievent.Event {
	userID := firstNonEmpty(valueAsString(data["openid"]), valueAsString(data["user_openid"]))
	userValue := &user.User{Id: userID}
	if userID == "" {
		userValue = nil
	}
	return &satorievent.Event{
		Type:      eventType,
		Timestamp: pickEventTimestamp(data),
		Login:     loginValue,
		User:      userValue,
	}
}
