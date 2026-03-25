package event

import (
	"github.com/satori-protocol-go/satori-go/pkg/satori/model/channel"
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
	channelValue := &channel.Channel{
		Id:   "private:" + userID,
		Type: channel.ChannelTypeDirect,
	}
	if userID == "" {
		channelValue = nil
	}
	return &satorievent.Event{
		Type:      eventType,
		Timestamp: pickEventTimestamp(data),
		Login:     loginValue,
		Channel:   channelValue,
		User:      userValue,
		Referrer: map[string]any{
			"event_id": valueAsString(data["id"]),
		},
	}
}
