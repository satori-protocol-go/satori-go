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
	EventID   string
	AppID     string
	Scene     dto.MessageScene
	Raw       map[string]any
	MsgSeq    int
	HasMsgSeq bool
}

type messageCreateInput struct {
	Platform  string
	ChannelID string
	Content   string
	Segments  []convert.MessageSegment
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
	input.Referrer.Direct = input.Referrer.Direct || strings.Contains(input.ChannelID, "_")
	if s.api == nil {
		return []*message.Message{}, nil
	}

	segments := input.Segments
	seq := newSeqCounter(input.Referrer)
	result := make([]*message.Message, 0, len(segments))
	for _, segment := range segments {
		if seq.Current() >= int(^uint32(0)) {
			return result, server.BadRequest("QQ reply sequence is exhausted")
		}

		piece := input
		piece.Referrer.MsgSeq = seq.Next()
		msg, err := s.sendQQGuildSegment(ctx, piece, segment)
		if err != nil {
			return result, err
		}
		if msg != nil {
			if msg.Id == "" {
				return result, errors.New("QQ send response has no message ID")
			}
			s.attachReplyContext(msg, input.Referrer, seq.Current())
			result = append(result, msg)
		}
	}
	return result, nil
}

func (s *messageSender) sendQQGuildSegment(ctx context.Context, input messageCreateInput, segment convert.MessageSegment) (*message.Message, error) {
	payload := &dto.MessageToCreate{MsgID: input.Referrer.MsgID, EventID: input.Referrer.EventID, MsgSeq: uint32(input.Referrer.MsgSeq), MessageReference: makeMessageReference(segment.QuoteID)}
	var created *dto.Message
	var err error
	if segment.Resource == nil {
		payload.Content = segment.Text
		if strings.TrimSpace(payload.Content) == "" {
			return nil, nil
		}
		created, err = s.callQQGuildMessageAPI(ctx, input.ChannelID, input.Referrer.Direct, payload)
	} else {
		resource, resolveErr := s.resolveResource(ctx, "qqguild", segment.Resource)
		if resolveErr != nil {
			return nil, resolveErr
		}
		if segment.Resource.Kind != convert.MessageResourceImage {
			if resource.URL == "" {
				return nil, server.NewActionError(501, "QQ guild local resource type is not supported", nil)
			}
			payload.Content = resource.URL
			created, err = s.callQQGuildMessageAPI(ctx, input.ChannelID, input.Referrer.Direct, payload)
		} else if resource.URL != "" {
			payload.Image = resource.URL
			created, err = s.callQQGuildMessageAPI(ctx, input.ChannelID, input.Referrer.Direct, payload)
		} else {
			created, err = s.callQQGuildMultipartAPI(ctx, input.ChannelID, input.Referrer.Direct, payload, resource.Data)
		}
	}
	if err != nil {
		return nil, err
	}
	if created == nil || created.ID == "" {
		return nil, errors.New("QQ send response has no message ID")
	}
	return s.convertMessage(created, "qqguild"), nil
}

func (s *messageSender) resolveResource(ctx context.Context, platform string, resource *convert.MessageResource) (convert.MessageResourcePayload, error) {
	if err := ctx.Err(); err != nil {
		return convert.MessageResourcePayload{}, err
	}
	payload, err := convert.ResolveMessageResourcePayload(resource.Src)
	if err != nil {
		return payload, server.BadRequest("invalid resource: " + err.Error())
	}
	if payload.Internal != "" {
		parts := strings.SplitN(strings.TrimPrefix(payload.Internal, "internal:"), "/", 3)
		if len(parts) != 3 || parts[0] != platform || parts[1] != s.state.selfID {
			return payload, server.Forbidden("resource belongs to a different login")
		}
		s.adapter.mu.RLock()
		owner := s.adapter.srv
		s.adapter.mu.RUnlock()
		if owner == nil {
			return payload, server.NewActionError(503, "the resource owner is not attached", nil)
		}
		payload.Data, err = owner.GetLocalFile(payload.Internal)
		if err != nil {
			return payload, err
		}
	}
	if resource.Title != "" {
		payload.FileName = resource.Title
	}
	return payload, nil
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
	return s.awaitAudit(ctx, err, payload.Content)
}

func (s *messageSender) callQQGuildMultipartAPI(ctx context.Context, channelID string, referrerDirect bool, payload *dto.MessageToCreate, data []byte) (*dto.Message, error) {
	if strings.Contains(channelID, "_") || referrerDirect {
		return nil, server.NewActionError(501, "local-image multipart is not verified for QQ guild direct messages", nil)
	}
	created, err := s.api.PostMessageMultipart(ctx, channelID, payload, data)
	if err == nil {
		return created, nil
	}
	return s.awaitAudit(ctx, err, payload.Content)
}

func makeMessageReference(messageID string) *dto.MessageReference {
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

	segments := input.Segments
	seq := newSeqCounter(input.Referrer)
	targetID, privateTarget := convert.SplitPrivateChannelID(input.ChannelID)
	isDirect := input.Referrer.Direct || privateTarget
	input.Referrer.Direct = isDirect

	result := make([]*message.Message, 0, len(segments))
	for _, segment := range segments {
		if seq.Current() >= int(^uint32(0)) {
			return result, server.BadRequest("QQ reply sequence is exhausted")
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
			return result, err
		}
		if created == nil || created.ID == "" {
			return result, errors.New("QQ send response has no message ID")
		}
		converted := s.convertMessage(created, "qq")
		s.attachReplyContext(converted, input.Referrer, seq.Current())
		result = append(result, converted)
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
		MsgType:          3,
		Ark:              ark,
		MsgID:            referrer.MsgID,
		EventID:          referrer.EventID,
		MessageReference: replyReference(referrer, segment.QuoteID),
		MsgSeq:           uint32(seq),
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
		MsgType:          2,
		MsgID:            referrer.MsgID,
		EventID:          referrer.EventID,
		MessageReference: replyReference(referrer, segment.QuoteID),
		MsgSeq:           uint32(seq),
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
		Content:          content,
		MsgID:            referrer.MsgID,
		EventID:          referrer.EventID,
		MessageReference: replyReference(referrer, segment.QuoteID),
		MsgSeq:           uint32(seq),
	}
	return s.callQQMessageAPI(ctx, targetID, isDirect, payload)
}

func (s *messageSender) sendQQResource(ctx context.Context, targetID string, isDirect bool, segment convert.MessageSegment, referrer messageReferrer, seq int) (*dto.Message, error) {
	if segment.Resource == nil {
		return nil, errors.New("QQ resource is required")
	}
	resource, err := s.resolveResource(ctx, "qq", segment.Resource)
	if err != nil {
		return nil, err
	}
	fileType := int(convert.MapMessageResourceFileType(segment.Resource.Kind))
	var uploaded *dto.MediaUploadResult
	if resource.Data != nil {
		data := resource.Data
		scope := media.GroupScope
		if isDirect {
			scope = media.C2CScope
		}
		uploaded, err = s.state.uploader.Upload(ctx, media.Target{Scope: scope, OpenID: targetID}, bytes.NewReader(data), int64(len(data)), fileType, resource.FileName)
	} else {
		request := &dto.MediaUploadRequest{FileType: fileType, URL: resource.URL, FileName: resource.FileName}
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
	payload := &dto.MessageToCreate{Content: " ", MsgType: dto.RichMediaMsg, MsgID: referrer.MsgID, EventID: referrer.EventID, MessageReference: replyReference(referrer, segment.QuoteID), MsgSeq: uint32(seq), Media: &dto.MediaInfo{FileInfo: uploaded.FileInfo}}
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
	content := ""
	if message, ok := payload.(*dto.MessageToCreate); ok {
		content = message.Content
	}
	return s.awaitAudit(ctx, err, content)
}

func (s *messageSender) awaitAudit(ctx context.Context, err error, content string) (*dto.Message, error) {
	var pending *errs.PendingError
	if !errors.As(err, &pending) || pending.AuditID == "" {
		return nil, err
	}
	result, waitErr := s.adapter.waitAuditResult(ctx, s.state.appID, pending.AuditID, defaultAuditWait)
	if waitErr != nil {
		status := 503
		if errors.Is(waitErr, context.DeadlineExceeded) {
			status = 504
		}
		return nil, server.NewActionError(status, "QQ audit outcome remains pending: "+waitErr.Error(), errors.Join(pending, waitErr))
	}
	if !result.Passed {
		return nil, server.NewActionError(403, "QQ message audit rejected: "+result.Reason, pending)
	}
	if result.MessageID == "" {
		return nil, server.NewActionError(502, "QQ audit passed without a message ID", pending)
	}
	return &dto.Message{ID: result.MessageID, Content: content, ChannelID: result.ChannelID, GuildID: result.GuildID}, nil
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

func replyReference(referrer messageReferrer, quoteID string) *dto.MessageReference {
	if quoteID == referrer.MsgID {
		if index, ok := referrer.Scene.GetExt("msg_idx"); ok {
			quoteID = index
		}
	}
	return makeMessageReference(quoteID)
}

func (s *messageSender) attachReplyContext(result *message.Message, referrer messageReferrer, sequence int) {
	if result == nil {
		return
	}
	output := make(map[string]any, len(referrer.Raw)+5)
	for key, value := range referrer.Raw {
		output[key] = value
	}
	output["msg_id"] = referrer.MsgID
	output["event_id"] = referrer.EventID
	output["msg_seq"] = sequence
	output["direct"] = referrer.Direct
	output["app_id"] = s.state.appID
	delete(output, "ref_idx")
	if index, ok := result.Referrer["ref_idx"]; ok {
		output["ref_idx"] = index
	}
	result.Referrer = output
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
