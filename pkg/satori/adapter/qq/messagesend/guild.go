package messagesend

import (
	"context"
	"strings"

	botgodto "github.com/WindowsSov8forUs/botgo-plus/dto"
	"github.com/satori-protocol-go/satori-go/pkg/satori/adapter/qq/messagecodec"
	"github.com/satori-protocol-go/satori-go/pkg/satori/model/message"
)

func (s *Sender) sendQQGuild(ctx context.Context, input CreateInput) ([]*message.Message, error) {
	if s.guildAPI == nil {
		return []*message.Message{}, nil
	}

	segments := parseSegments(input.Content, "qqguild")
	seq := newSeqCounter(input.Referrer)
	result := make([]*message.Message, 0, len(segments))
	for _, segment := range segments {
		if input.Referrer.MsgID != "" && seq.Current() >= 5 {
			break
		}

		msg, err := s.sendQQGuildSegment(ctx, input, segment)
		if err != nil {
			return nil, err
		}
		if msg != nil {
			result = append(result, msg)
			seq.Next()
		}
	}
	return result, nil
}

func (s *Sender) sendQQGuildSegment(
	ctx context.Context,
	input CreateInput,
	segment messagecodec.Segment,
) (*message.Message, error) {
	messageReference := makeMessageReference(segment.QuoteID)
	payload := &botgodto.MessageToCreate{
		MsgID:            input.Referrer.MsgID,
		MessageReference: messageReference,
	}

	if segment.Resource == nil {
		payload.Content = segment.Text
		if strings.TrimSpace(payload.Content) == "" {
			return nil, nil
		}
		created, err := s.callQQGuildMessageAPI(ctx, input.ChannelID, input.Referrer.Direct, payload)
		if err != nil {
			return nil, err
		}
		if created == nil || s.convertMessage == nil {
			return nil, nil
		}
		return s.convertMessage(created, "qqguild"), nil
	}

	if segment.Resource.Kind != messagecodec.ResourceImage {
		payload.Content = segment.Resource.Src
		if strings.TrimSpace(payload.Content) == "" {
			return nil, nil
		}
		created, err := s.callQQGuildMessageAPI(ctx, input.ChannelID, input.Referrer.Direct, payload)
		if err != nil {
			return nil, err
		}
		if created == nil || s.convertMessage == nil {
			return nil, nil
		}
		return s.convertMessage(created, "qqguild"), nil
	}

	resourcePayload, err := resolveResourcePayload(segment.Resource.Src)
	if err != nil {
		payload.Content = segment.Resource.Src
		created, callErr := s.callQQGuildMessageAPI(ctx, input.ChannelID, input.Referrer.Direct, payload)
		if callErr != nil {
			return nil, callErr
		}
		if created == nil || s.convertMessage == nil {
			return nil, nil
		}
		return s.convertMessage(created, "qqguild"), nil
	}

	switch {
	case resourcePayload.URL != "":
		payload.Image = resourcePayload.URL
		created, callErr := s.callQQGuildMessageAPI(ctx, input.ChannelID, input.Referrer.Direct, payload)
		if callErr != nil {
			return nil, callErr
		}
		if created == nil || s.convertMessage == nil {
			return nil, nil
		}
		return s.convertMessage(created, "qqguild"), nil
	case resourcePayload.FileData != "" && s.guildMultipartAPI != nil:
		data, decodeErr := decodeBase64Data(resourcePayload.FileData)
		if decodeErr != nil {
			return nil, decodeErr
		}
		created, callErr := s.callQQGuildMultipartAPI(
			ctx,
			input.ChannelID,
			input.Referrer.Direct,
			payload,
			data,
		)
		if callErr != nil {
			return nil, callErr
		}
		if created == nil || s.convertMessage == nil {
			return nil, nil
		}
		return s.convertMessage(created, "qqguild"), nil
	default:
		payload.Content = segment.Resource.Src
		if strings.TrimSpace(payload.Content) == "" {
			return nil, nil
		}
		created, callErr := s.callQQGuildMessageAPI(ctx, input.ChannelID, input.Referrer.Direct, payload)
		if callErr != nil {
			return nil, callErr
		}
		if created == nil || s.convertMessage == nil {
			return nil, nil
		}
		return s.convertMessage(created, "qqguild"), nil
	}
}

func (s *Sender) callQQGuildMessageAPI(
	ctx context.Context,
	channelID string,
	referrerDirect bool,
	payload *botgodto.MessageToCreate,
) (*botgodto.Message, error) {
	if strings.Contains(channelID, "_") || referrerDirect {
		dmGuildID := channelID
		if strings.Contains(dmGuildID, "_") {
			dmGuildID = strings.SplitN(dmGuildID, "_", 2)[0]
		}
		return s.guildAPI.PostDirectMessage(ctx, &botgodto.DirectMessage{GuildID: dmGuildID}, payload)
	}
	return s.guildAPI.PostMessage(ctx, channelID, payload)
}

func (s *Sender) callQQGuildMultipartAPI(
	ctx context.Context,
	channelID string,
	referrerDirect bool,
	payload *botgodto.MessageToCreate,
	fileImageData []byte,
) (*botgodto.Message, error) {
	if s.guildMultipartAPI == nil {
		return s.callQQGuildMessageAPI(ctx, channelID, referrerDirect, payload)
	}
	if strings.Contains(channelID, "_") || referrerDirect {
		dmGuildID := channelID
		if strings.Contains(dmGuildID, "_") {
			dmGuildID = strings.SplitN(dmGuildID, "_", 2)[0]
		}
		return s.guildMultipartAPI.PostDirectMessageMultipart(
			ctx,
			&botgodto.DirectMessage{GuildID: dmGuildID},
			payload,
			fileImageData,
		)
	}
	return s.guildMultipartAPI.PostMessageMultipart(ctx, channelID, payload, fileImageData)
}

func makeMessageReference(messageID string) *botgodto.MessageReference {
	messageID = strings.TrimSpace(messageID)
	if messageID == "" {
		return nil
	}
	return &botgodto.MessageReference{
		MessageID:             messageID,
		IgnoreGetMessageError: true,
	}
}
