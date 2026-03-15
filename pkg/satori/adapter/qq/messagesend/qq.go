package messagesend

import (
	"context"
	"strings"

	botgodto "github.com/WindowsSov8forUs/botgo-plus/dto"
	"github.com/satori-protocol-go/satori-go/pkg/satori/adapter/qq/messagecodec"
	"github.com/satori-protocol-go/satori-go/pkg/satori/model/message"
)

func (s *Sender) sendQQ(ctx context.Context, input CreateInput) ([]*message.Message, error) {
	if s.qqAPI == nil {
		return []*message.Message{}, nil
	}

	segments := parseSegments(input.Content, "qq")
	seq := newSeqCounter(input.Referrer)
	isDirect := input.Referrer.Direct || strings.HasPrefix(input.ChannelID, "private:")
	targetID := input.ChannelID
	if strings.HasPrefix(targetID, "private:") {
		targetID = strings.TrimPrefix(targetID, "private:")
	}

	result := make([]*message.Message, 0, len(segments))
	for _, segment := range segments {
		if input.Referrer.MsgID != "" && seq.Current() >= 5 {
			break
		}
		var created *botgodto.Message
		var err error
		if segment.Resource == nil {
			created, err = s.sendQQText(ctx, targetID, isDirect, segment, input.Referrer, seq.Next())
		} else {
			created, err = s.sendQQResource(ctx, targetID, isDirect, segment, input.Referrer, seq.Next())
		}
		if err != nil {
			return nil, err
		}
		if created == nil || s.convertMessage == nil {
			continue
		}
		result = append(result, s.convertMessage(created, "qq"))
	}
	return result, nil
}

func (s *Sender) sendQQText(
	ctx context.Context,
	targetID string,
	isDirect bool,
	segment messagecodec.Segment,
	referrer Referrer,
	seq int,
) (*botgodto.Message, error) {
	content := segment.Text
	if strings.TrimSpace(content) == "" {
		return nil, nil
	}
	payload := &botgodto.MessageToCreate{
		Content: content,
		MsgID:   resolveQQMsgID(referrer.MsgID, segment.QuoteID),
		MsgSeq:  seq,
	}
	return s.callQQMessageAPI(ctx, targetID, isDirect, payload)
}

func (s *Sender) sendQQResource(
	ctx context.Context,
	targetID string,
	isDirect bool,
	segment messagecodec.Segment,
	referrer Referrer,
	seq int,
) (*botgodto.Message, error) {
	if segment.Resource == nil {
		return nil, nil
	}

	resourcePayload, err := resolveResourcePayload(segment.Resource.Src)
	if err != nil {
		return s.sendQQText(ctx, targetID, isDirect, messagecodec.Segment{Text: segment.Resource.Src}, referrer, seq)
	}
	if resourcePayload.URL == "" && resourcePayload.FileData == "" {
		return s.sendQQText(ctx, targetID, isDirect, messagecodec.Segment{Text: segment.Resource.Src}, referrer, seq)
	}

	uploadMessage := &botgodto.RichMediaMessage{
		EventID:    resolveQQMsgID(referrer.MsgID, segment.QuoteID),
		FileType:   resourceFileType(segment.Resource.Kind),
		URL:        resourcePayload.URL,
		FileData:   resourcePayload.FileData,
		SrvSendMsg: false,
	}

	var mediaResponse *botgodto.MediaResponse
	if isDirect {
		response, callErr := s.qqAPI.PostC2CMessage(ctx, targetID, uploadMessage)
		if callErr != nil {
			return nil, callErr
		}
		if response != nil {
			mediaResponse = response.MediaResponse
		}
	} else {
		response, callErr := s.qqAPI.PostGroupMessage(ctx, targetID, uploadMessage)
		if callErr != nil {
			return nil, callErr
		}
		if response != nil {
			mediaResponse = response.MediaResponse
		}
	}
	if mediaResponse == nil || strings.TrimSpace(mediaResponse.FileInfo) == "" {
		return s.sendQQText(ctx, targetID, isDirect, messagecodec.Segment{Text: segment.Resource.Src}, referrer, seq)
	}

	payload := &botgodto.MessageToCreate{
		Content: " ",
		MsgType: 7,
		MsgID:   resolveQQMsgID(referrer.MsgID, segment.QuoteID),
		MsgSeq:  seq,
		Media:   botgodto.Media{FileInfo: mediaResponse.FileInfo},
	}
	return s.callQQMessageAPI(ctx, targetID, isDirect, payload)
}

func (s *Sender) callQQMessageAPI(
	ctx context.Context,
	targetID string,
	isDirect bool,
	payload botgodto.APIMessage,
) (*botgodto.Message, error) {
	if isDirect {
		response, err := s.qqAPI.PostC2CMessage(ctx, targetID, payload)
		if err != nil {
			return nil, err
		}
		if response == nil {
			return nil, nil
		}
		return response.Message, nil
	}

	response, err := s.qqAPI.PostGroupMessage(ctx, targetID, payload)
	if err != nil {
		return nil, err
	}
	if response == nil {
		return nil, nil
	}
	return response.Message, nil
}

func resolveQQMsgID(defaultMsgID string, quoteID string) string {
	quoteID = strings.TrimSpace(quoteID)
	if quoteID != "" {
		return quoteID
	}
	return strings.TrimSpace(defaultMsgID)
}
