package qq

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/WindowsSov8forUs/botgo-plus/dto"
	"github.com/WindowsSov8forUs/botgo-plus/dto/keyboard"
	"github.com/WindowsSov8forUs/botgo-plus/errs"
	"github.com/WindowsSov8forUs/botgo-plus/media"
	native "github.com/WindowsSov8forUs/botgo-plus/openapi/v1"
	"github.com/satori-protocol-go/satori-go/pkg/satori/adapter/qq/convert"
	"github.com/satori-protocol-go/satori-go/pkg/satori/model/message"
	"github.com/satori-protocol-go/satori-go/pkg/satori/server"
)

var errUnsupportedPlatform = errors.New("unsupported platform")

type messageConverter func(input *dto.Message, platform string) *message.Message

type messageSender struct {
	api            *native.Client
	state          *appState
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

func newMessageSender(state *appState, convert messageConverter, adapter *Adapter) *messageSender {
	return &messageSender{api: state.api, state: state, convertMessage: convert, adapter: adapter}
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
	if s.api == nil {
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
		created, err = s.api.PostDirectMessage(ctx, &dto.DirectMessage{GuildID: dmGuildID}, payload)
	} else {
		created, err = s.api.PostMessage(ctx, channelID, payload)
	}
	if err == nil {
		return created, nil
	}
	if fallback, ok := s.tryAuditFallback(ctx, err, payload.Content); ok {
		return fallback, nil
	}
	return nil, err
}

func (s *messageSender) callQQGuildMultipartAPI(ctx context.Context, channelID string, referrerDirect bool, payload *dto.MessageToCreate, data []byte) (*dto.Message, error) {
	if strings.Contains(channelID, "_") || referrerDirect {
		return nil, server.NewActionError(501, "local-image multipart is not verified for QQ guild direct messages", nil)
	}
	created, err := s.api.PostMessageMultipart(ctx, channelID, payload, data)
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
	if s.api == nil {
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
		MsgSeq:  uint32(seq),
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
		MsgSeq:  uint32(seq),
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
		MsgSeq:  uint32(seq),
	}
	return s.callQQMessageAPI(ctx, targetID, isDirect, payload)
}

func (s *messageSender) sendQQResource(ctx context.Context, targetID string, isDirect bool, segment convert.MessageSegment, referrer messageReferrer, seq int) (*dto.Message, error) {
	if segment.Resource == nil {
		return nil, errors.New("QQ resource is required")
	}
	resource, err := convert.ResolveMessageResourcePayload(segment.Resource.Src)
	if err != nil {
		return nil, err
	}
	fileType := int(convert.MapMessageResourceFileType(segment.Resource.Kind))
	var uploaded *dto.MediaUploadResult
	if resource.FileData != "" {
		data, decodeErr := convert.DecodeMessageBase64(resource.FileData)
		if decodeErr != nil {
			return nil, decodeErr
		}
		scope := media.GroupScope
		if isDirect {
			scope = media.C2CScope
		}
		uploaded, err = s.state.uploader.Upload(ctx, media.Target{Scope: scope, OpenID: targetID}, bytes.NewReader(data), int64(len(data)), fileType, "upload")
	} else {
		request := &dto.MediaUploadRequest{FileType: fileType, URL: resource.URL, FileName: "upload"}
		if isDirect {
			uploaded, _, err = s.api.UploadC2CFile(ctx, targetID, request)
		} else {
			uploaded, _, err = s.api.UploadGroupFile(ctx, targetID, request)
		}
	}
	if err != nil {
		return nil, err
	}
	if uploaded == nil || uploaded.FileInfo == "" {
		return nil, errors.New("QQ upload response has no file_info")
	}
	payload := &dto.MessageToCreate{Content: " ", MsgType: dto.RichMediaMsg, MsgID: resolveQQMsgID(referrer.MsgID, segment.QuoteID), MsgSeq: uint32(seq), Media: &dto.MediaInfo{FileInfo: uploaded.FileInfo}}
	return s.callQQMessageAPI(ctx, targetID, isDirect, payload)
}

func (s *messageSender) callQQMessageAPI(ctx context.Context, targetID string, isDirect bool, payload dto.APIMessage) (*dto.Message, error) {
	var created *dto.Message
	var err error
	if isDirect {
		created, err = s.api.PostC2CMessage(ctx, targetID, payload)
	} else {
		created, err = s.api.PostGroupMessage(ctx, targetID, payload)
	}
	if err == nil {
		return created, nil
	}
	if fallback, ok := s.tryAuditFallback(ctx, err, ""); ok {
		return fallback, nil
	}
	return nil, err
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
	var pending *errs.PendingError
	if errors.As(err, &pending) && pending.AuditID != "" {
		return pending.AuditID, true
	}
	return "", false
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
