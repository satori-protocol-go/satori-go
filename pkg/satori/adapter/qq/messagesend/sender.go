package messagesend

import (
	"context"
	"errors"
	"strings"

	"github.com/satori-protocol-go/satori-go/pkg/satori/model/message"
)

var ErrUnsupportedPlatform = errors.New("unsupported platform")

func (s *Sender) Send(ctx context.Context, input CreateInput) ([]*message.Message, error) {
	platform := strings.TrimSpace(input.Platform)
	switch platform {
	case "qqguild":
		return s.sendQQGuild(ctx, input)
	case "qq":
		return s.sendQQ(ctx, input)
	default:
		return nil, ErrUnsupportedPlatform
	}
}
