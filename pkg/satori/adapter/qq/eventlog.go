package qq

import (
	"context"
	"fmt"
	"github.com/WindowsSov8forUs/botgo-plus/dto"
	"github.com/satori-protocol-go/satori-go/pkg/satori/logging"
	"github.com/satori-protocol-go/satori-go/pkg/satori/model/event"
)

// Log accepted event metadata. Applications can log message content explicitly
// in their own handler; native envelopes and resource URLs are not log fields.
func (a *Adapter) logEventBySource(ctx context.Context, typ dto.EventType, evt *event.Event) {
	platform, selfID, channelID, messageID := "", "", "", ""
	if evt.Login != nil {
		platform = evt.Login.Platform
		if evt.Login.User != nil {
			selfID = evt.Login.User.Id
		}
	}
	if evt.Channel != nil {
		channelID = evt.Channel.Id
	}
	if evt.Message != nil {
		messageID = evt.Message.Id
	}
	a.log(ctx, logging.LevelInfo, fmt.Sprintf("QQ event accepted app_id=%q type=%q platform=%q self_id=%q channel_id=%q message_id=%q", appIDFromContext(ctx), typ, platform, selfID, channelID, messageID))
}
