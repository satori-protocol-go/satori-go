package qq

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/WindowsSov8forUs/botgo-plus/dto"
	"github.com/WindowsSov8forUs/botgo-plus/dto/keyboard"
	"github.com/WindowsSov8forUs/botgo-plus/errs"
	"github.com/WindowsSov8forUs/botgo-plus/openapi"
	"github.com/satori-protocol-go/satori-go/pkg/satori/adapter/qq/convert"
	"github.com/satori-protocol-go/satori-go/pkg/satori/model/message"
)

var errUnsupportedPlatform = errors.New("unsupported platform")

type messageConverter func(input *dto.Message, platform string) *message.Message

type messageSender struct {
	apiV1          openapi.OpenAPI
	apiV2          openapi.OpenAPI
	convertMessage messageConverter
	adapter        *Adapter
}

type messageReferrer struct {
	Direct    bool
	MsgID     string
	MsgSeq    int
	HasMsgSeq bool
}

type messageCreateInput struct {
	Platform  string
	ChannelID string
	Content   string
	Referrer  messageReferrer
}

var markdownEscapePattern = regexp.MustCompile("([\\\\`*_{}\\[\\]()#+\\-.!>~])")

func newMessageSender(apiV1 openapi.OpenAPI, apiV2 openapi.OpenAPI, convert messageConverter, adapter *Adapter) *messageSender {
	return &messageSender{
		apiV1:          apiV1,
		apiV2:          apiV2,
		convertMessage: convert,
		adapter:        adapter,
	}
}

func (s *messageSender) Send(ctx context.Context, input messageCreateInput) ([]*message.Message, error) {
	switch input.Platform {
	case "qqguild":
		return s.sendQQGuild(ctx, input)
	case "qq":
		return s.sendQQ(ctx, input)
	default:
		return nil, errUnsupportedPlatform
	}
}

func (s *messageSender) sendQQGuild(ctx context.Context, input messageCreateInput) ([]*message.Message, error) {
	if s.apiV1 == nil {
		return []*message.Message{}, nil
	}

	segments := convert.ParseMessageSegments(input.Content, "qqguild")
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

func (s *messageSender) sendQQGuildSegment(
	ctx context.Context,
	input messageCreateInput,
	segment convert.MessageSegment,
) (*message.Message, error) {
	payload := &dto.MessageToCreate{
		MsgID:            input.Referrer.MsgID,
		MessageReference: makeMessageReference(segment.QuoteID),
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

	if segment.Resource.Kind != convert.MessageResourceImage {
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

	resourcePayload, err := convert.ResolveMessageResourcePayload(segment.Resource.Src)
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
	case resourcePayload.FileData != "":
		data, decodeErr := convert.DecodeMessageBase64(resourcePayload.FileData)
		if decodeErr != nil {
			return nil, decodeErr
		}
		created, callErr := s.callQQGuildMultipartAPI(ctx, input.ChannelID, input.Referrer.Direct, payload, data)
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

func (s *messageSender) callQQGuildMessageAPI(
	ctx context.Context,
	channelID string,
	referrerDirect bool,
	payload *dto.MessageToCreate,
) (*dto.Message, error) {
	var (
		created *dto.Message
		err     error
	)
	if strings.Contains(channelID, "_") || referrerDirect {
		dmGuildID := convert.SplitGuildCompositeID(channelID)
		created, err = s.apiV1.PostDirectMessage(ctx, &dto.DirectMessage{GuildID: dmGuildID}, payload)
	} else {
		created, err = s.apiV1.PostMessage(ctx, channelID, payload)
	}
	if err == nil {
		return created, nil
	}
	if fallback, ok := s.tryAuditFallback(ctx, err, payload.Content); ok {
		return fallback, nil
	}
	return nil, err
}

func (s *messageSender) callQQGuildMultipartAPI(
	ctx context.Context,
	channelID string,
	referrerDirect bool,
	payload *dto.MessageToCreate,
	fileImageData []byte,
) (*dto.Message, error) {
	if s.apiV1 == nil {
		return s.callQQGuildMessageAPI(ctx, channelID, referrerDirect, payload)
	}
	var (
		created *dto.Message
		err     error
	)
	if strings.Contains(channelID, "_") || referrerDirect {
		dmGuildID := convert.SplitGuildCompositeID(channelID)
		created, err = s.apiV1.PostDirectMessageMultipart(ctx, &dto.DirectMessage{GuildID: dmGuildID}, payload, fileImageData)
	} else {
		created, err = s.apiV1.PostMessageMultipart(ctx, channelID, payload, fileImageData)
	}
	if err == nil {
		return created, nil
	}
	if fallback, ok := s.tryAuditFallback(ctx, err, payload.Content); ok {
		return fallback, nil
	}
	return nil, err
}

func makeMessageReference(messageID string) *dto.MessageReference {
	messageID = strings.TrimSpace(messageID)
	if messageID == "" {
		return nil
	}
	return &dto.MessageReference{
		MessageID:             messageID,
		IgnoreGetMessageError: true,
	}
}

func (s *messageSender) sendQQ(ctx context.Context, input messageCreateInput) ([]*message.Message, error) {
	if s.apiV2 == nil {
		return []*message.Message{}, nil
	}

	segments := convert.ParseMessageSegments(input.Content, "qq")
	seq := newSeqCounter(input.Referrer)
	targetID, privateTarget := convert.SplitPrivateChannelID(input.ChannelID)
	isDirect := input.Referrer.Direct || privateTarget

	result := make([]*message.Message, 0, len(segments))
	for _, segment := range segments {
		if input.Referrer.MsgID != "" && seq.Current() >= 5 {
			break
		}
		var created *dto.Message
		var err error
		switch {
		case segment.ArkJSON != "":
			created, err = s.sendQQArk(ctx, targetID, isDirect, segment, input.Referrer, seq.Next())
		case segment.Markdown || len(segment.Buttons) > 0:
			created, err = s.sendQQMarkdown(ctx, targetID, isDirect, segment, input.Referrer, seq.Next())
		case segment.Resource == nil:
			created, err = s.sendQQText(ctx, targetID, isDirect, segment, input.Referrer, seq.Next())
		default:
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

func (s *messageSender) sendQQArk(
	ctx context.Context,
	targetID string,
	isDirect bool,
	segment convert.MessageSegment,
	referrer messageReferrer,
	seq int,
) (*dto.Message, error) {
	ark := &dto.Ark{}
	if err := json.Unmarshal([]byte(segment.ArkJSON), ark); err != nil {
		return s.sendQQText(ctx, targetID, isDirect, convert.MessageSegment{Text: segment.ArkJSON}, referrer, seq)
	}
	payload := &dto.MessageToCreate{
		MsgType: 3,
		Ark:     ark,
		MsgID:   resolveQQMsgID(referrer.MsgID, segment.QuoteID),
		MsgSeq:  seq,
	}
	return s.callQQMessageAPI(ctx, targetID, isDirect, payload)
}

func (s *messageSender) sendQQMarkdown(
	ctx context.Context,
	targetID string,
	isDirect bool,
	segment convert.MessageSegment,
	referrer messageReferrer,
	seq int,
) (*dto.Message, error) {
	markdownContent := escapeQQMarkdown(segment.Text)
	if strings.TrimSpace(markdownContent) == "" {
		markdownContent = " "
	}
	payload := &dto.MessageToCreate{
		MsgType: 2,
		MsgID:   resolveQQMsgID(referrer.MsgID, segment.QuoteID),
		MsgSeq:  seq,
		Markdown: &dto.Markdown{
			Content: markdownContent,
		},
	}
	if len(segment.Buttons) > 0 {
		payload.Keyboard = buildKeyboardFromButtons(segment.Buttons)
	}
	return s.callQQMessageAPI(ctx, targetID, isDirect, payload)
}

func (s *messageSender) sendQQText(
	ctx context.Context,
	targetID string,
	isDirect bool,
	segment convert.MessageSegment,
	referrer messageReferrer,
	seq int,
) (*dto.Message, error) {
	content := segment.Text
	if strings.TrimSpace(content) == "" {
		return nil, nil
	}
	payload := &dto.MessageToCreate{
		Content: content,
		MsgID:   resolveQQMsgID(referrer.MsgID, segment.QuoteID),
		MsgSeq:  seq,
	}
	return s.callQQMessageAPI(ctx, targetID, isDirect, payload)
}

func (s *messageSender) sendQQResource(
	ctx context.Context,
	targetID string,
	isDirect bool,
	segment convert.MessageSegment,
	referrer messageReferrer,
	seq int,
) (*dto.Message, error) {
	if segment.Resource == nil {
		return nil, nil
	}

	resourcePayload, err := convert.ResolveMessageResourcePayload(segment.Resource.Src)
	if err != nil {
		return s.sendQQText(ctx, targetID, isDirect, convert.MessageSegment{Text: segment.Resource.Src}, referrer, seq)
	}
	if resourcePayload.URL == "" && resourcePayload.FileData == "" {
		return s.sendQQText(ctx, targetID, isDirect, convert.MessageSegment{Text: segment.Resource.Src}, referrer, seq)
	}

	uploadMessage := &dto.RichMediaMessage{
		EventID:    resolveQQMsgID(referrer.MsgID, segment.QuoteID),
		FileType:   convert.MapMessageResourceFileType(segment.Resource.Kind),
		URL:        resourcePayload.URL,
		FileData:   resourcePayload.FileData,
		SrvSendMsg: false,
	}

	var mediaResponse *dto.MediaResponse
	if isDirect {
		response, callErr := s.apiV2.PostC2CMessage(ctx, targetID, uploadMessage)
		if callErr != nil {
			return nil, callErr
		}
		if response != nil {
			mediaResponse = response.MediaResponse
		}
	} else {
		response, callErr := s.apiV2.PostGroupMessage(ctx, targetID, uploadMessage)
		if callErr != nil {
			return nil, callErr
		}
		if response != nil {
			mediaResponse = response.MediaResponse
		}
	}
	if mediaResponse == nil || strings.TrimSpace(mediaResponse.FileInfo) == "" {
		return s.sendQQText(ctx, targetID, isDirect, convert.MessageSegment{Text: segment.Resource.Src}, referrer, seq)
	}

	payload := &dto.MessageToCreate{
		Content: " ",
		MsgType: 7,
		MsgID:   resolveQQMsgID(referrer.MsgID, segment.QuoteID),
		MsgSeq:  seq,
		Media:   dto.Media{FileInfo: mediaResponse.FileInfo},
	}
	return s.callQQMessageAPI(ctx, targetID, isDirect, payload)
}

func (s *messageSender) callQQMessageAPI(
	ctx context.Context,
	targetID string,
	isDirect bool,
	payload dto.APIMessage,
) (*dto.Message, error) {
	if isDirect {
		response, err := s.apiV2.PostC2CMessage(ctx, targetID, payload)
		if err != nil {
			if fallback, ok := s.tryAuditFallback(ctx, err, ""); ok {
				return fallback, nil
			}
			return nil, err
		}
		if response == nil {
			return nil, nil
		}
		return response.Message, nil
	}

	response, err := s.apiV2.PostGroupMessage(ctx, targetID, payload)
	if err != nil {
		if fallback, ok := s.tryAuditFallback(ctx, err, ""); ok {
			return fallback, nil
		}
		return nil, err
	}
	if response == nil {
		return nil, nil
	}
	return response.Message, nil
}

func (s *messageSender) tryAuditFallback(ctx context.Context, err error, content string) (*dto.Message, bool) {
	if s == nil || s.adapter == nil {
		return nil, false
	}
	auditID, ok := parseAuditIDFromError(err)
	if !ok {
		return nil, false
	}
	messageID, ok := s.adapter.waitAuditMessageID(ctx, auditID, defaultAuditWait)
	if !ok {
		return nil, false
	}
	return &dto.Message{ID: messageID, Content: content}, true
}

func parseAuditIDFromError(err error) (string, bool) {
	if err == nil {
		return "", false
	}
	wrapped := errs.Error(err)
	if wrapped == nil {
		return "", false
	}
	if wrapped.Code() != http.StatusCreated && wrapped.Code() != http.StatusAccepted {
		return "", false
	}
	body := struct {
		Code int `json:"code"`
		Data struct {
			MessageAudit struct {
				AuditID string `json:"audit_id"`
			} `json:"message_audit"`
		} `json:"data"`
	}{}
	if json.Unmarshal([]byte(wrapped.Text()), &body) != nil {
		return "", false
	}
	if body.Code != 304023 {
		return "", false
	}
	if strings.TrimSpace(body.Data.MessageAudit.AuditID) == "" {
		return "", false
	}
	return strings.TrimSpace(body.Data.MessageAudit.AuditID), true
}

func escapeQQMarkdown(content string) string {
	return markdownEscapePattern.ReplaceAllString(content, `\\$1`)
}

func buildKeyboardFromButtons(rows [][]convert.MessageButton) *keyboard.MessageKeyboard {
	if len(rows) == 0 {
		return nil
	}
	resultRows := make([]*keyboard.Row, 0, len(rows))
	for rowIndex, row := range rows {
		buttons := make([]*keyboard.Button, 0, len(row))
		for buttonIndex, item := range row {
			buttonID := strings.TrimSpace(item.Id)
			if buttonID == "" {
				buttonID = fmt.Sprintf("btn_%d_%d_%d", time.Now().UnixNano(), rowIndex, buttonIndex)
			}
			label := strings.TrimSpace(item.Label)
			if label == "" {
				label = buttonID
			}
			actionType := keyboard.ActionTypeCallback
			actionData := buttonID
			switch strings.ToLower(strings.TrimSpace(item.Type)) {
			case "link":
				actionType = keyboard.ActionTypeURL
				actionData = strings.TrimSpace(item.Href)
			case "input":
				actionType = keyboard.ActionTypeAtBot
				actionData = strings.TrimSpace(item.Text)
			}
			style := 0
			if strings.EqualFold(strings.TrimSpace(item.Theme), "primary") {
				style = 1
			}
			if strings.TrimSpace(actionData) == "" {
				actionData = buttonID
			}
			buttons = append(buttons, &keyboard.Button{
				ID: buttonID,
				RenderData: &keyboard.RenderData{
					Label:        label,
					VisitedLabel: label,
					Style:        style,
				},
				Action: &keyboard.Action{
					Type: actionType,
					Permission: &keyboard.Permission{
						Type: keyboard.PermissionTypAll,
					},
					Data: actionData,
				},
			})
		}
		if len(buttons) > 0 {
			resultRows = append(resultRows, &keyboard.Row{Buttons: buttons})
		}
	}
	if len(resultRows) == 0 {
		return nil
	}
	return &keyboard.MessageKeyboard{
		Content: &keyboard.CustomKeyboard{
			Rows: resultRows,
		},
	}
}

func resolveQQMsgID(defaultMsgID string, quoteID string) string {
	quoteID = strings.TrimSpace(quoteID)
	if quoteID != "" {
		return quoteID
	}
	return strings.TrimSpace(defaultMsgID)
}

type seqCounter struct {
	value int
}

func newSeqCounter(referrer messageReferrer) *seqCounter {
	if referrer.HasMsgSeq {
		return &seqCounter{value: referrer.MsgSeq}
	}
	return &seqCounter{value: -1}
}

func (s *seqCounter) Current() int {
	return s.value
}

func (s *seqCounter) Next() int {
	s.value++
	return s.value
}
