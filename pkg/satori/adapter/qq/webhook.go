package qq

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"math"
	"net/http"
	"sort"
	"strings"

	"github.com/WindowsSov8forUs/botgo-plus/dto"
	"github.com/WindowsSov8forUs/botgo-plus/interaction/signature"
	"github.com/WindowsSov8forUs/botgo-plus/interaction/webhook"
	"github.com/go-chi/chi/v5"
	"github.com/satori-protocol-go/satori-go/pkg/satori/logging"
	"github.com/satori-protocol-go/satori-go/pkg/satori/model/event"
)

func (a *Adapter) RegisterRootRoutes(router chi.Router) {
	if router == nil {
		return
	}
	if a.wsEnabled {
		a.log(context.Background(), logging.LevelInfo, "qq adapter is in websocket mode, webhook routes are disabled")
		return
	}
	handler := http.HandlerFunc(a.handleWebhookRequest)
	for _, path := range normalizeWebhookPaths(a.path) {
		router.Handle(path, handler)
	}
	a.log(context.Background(), logging.LevelInfo, "qq webhook routes registered")
}

func normalizeWebhookPath(path string) string {
	path = strings.TrimSpace(path)
	if path == "" {
		return defaultWebhookPath
	}
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}
	return path
}

func normalizeWebhookPaths(path string) []string {
	path = normalizeWebhookPath(path)
	set := map[string]struct{}{path: {}}
	trimmed := strings.TrimSuffix(path, "/")
	if trimmed == "" {
		trimmed = "/"
	}
	set[trimmed] = struct{}{}

	result := make([]string, 0, len(set))
	for item := range set {
		result = append(result, item)
	}
	sort.Strings(result)
	return result
}

func writeJSON(w http.ResponseWriter, payload any) {
	data, err := json.Marshal(payload)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(data)
}

func (a *Adapter) handleWebhookRequest(w http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodPost {
		http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
		return
	}

	appID := strings.TrimSpace(request.Header.Get("X-Bot-Appid"))
	state := a.stateByAppID(appID)
	if appID == "" || state == nil {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	if a.skipSignatureCheck {
		a.handleWebhookRequestWithoutSignature(w, request, appID, state.secret)
		return
	}

	handler := webhook.HTTPHandlerWithOptions(webhook.HandlerOptions{
		GetSecret: func(r *http.Request) string {
			return state.secret
		},
		ParsePayload: func(payload *dto.Payload, _ string) (string, error) {
			return a.parseWebhookPayloadResponse(withAppID(request.Context(), appID), appID, state.secret, payload)
		},
	})
	handler(w, request)
}

func (a *Adapter) handleWebhookRequestWithoutSignature(
	w http.ResponseWriter,
	request *http.Request,
	appID string,
	secret string,
) {
	body, err := io.ReadAll(request.Body)
	if err != nil {
		http.Error(w, "failed to read request body", http.StatusBadRequest)
		return
	}
	payload, rawData, err := parseWebhookPayload(body)
	if err != nil {
		http.Error(w, "invalid payload", http.StatusBadRequest)
		return
	}
	switch payload.OPCode {
	case dto.HTTPCallbackValidation:
		response, validationErr := buildWebhookValidationResponse(secret, rawData)
		if validationErr != nil {
			http.Error(w, validationErr.Error(), http.StatusBadRequest)
			return
		}
		writeJSON(w, response)
	default:
		result, parseErr := a.parseWebhookPayloadResponse(withAppID(request.Context(), appID), appID, secret, payload)
		if parseErr != nil {
			http.Error(w, parseErr.Error(), http.StatusBadRequest)
			return
		}
		if strings.TrimSpace(result) == "" {
			w.WriteHeader(http.StatusOK)
			return
		}
		_, _ = w.Write([]byte(result))
	}
}

func (a *Adapter) parseWebhookPayloadResponse(
	ctx context.Context,
	appID string,
	secret string,
	payload *dto.Payload,
) (string, error) {
	rawData := payloadDataFromEvent(payload)
	switch payload.OPCode {
	case dto.HTTPCallbackValidation:
		response, err := buildWebhookValidationResponse(secret, rawData)
		if err != nil {
			return "", err
		}
		encoded, err := json.Marshal(response)
		if err != nil {
			return "", err
		}
		return string(encoded), nil
	case dto.WSHeartbeat:
		return webhook.GenHeartbeatACK(webhookHeartbeatACKSeq(payload)), nil
	case dto.DispatchEvent:
		if strings.HasPrefix(string(payload.Type), "MESSAGE_AUDIT_") {
			a.captureAuditResult(rawData)
		}
		evt, convertErr := a.convertWebhookPayload(withAppID(ctx, appID), payload.OPCode, payload.Type, rawData)
		if convertErr != nil {
			return webhook.GenDispatchACK(false), nil
		}
		if evt != nil {
			a.logEventBySource(payload.Type, evt)
			a.pushEvent(evt)
		}
		return webhook.GenDispatchACK(true), nil
	default:
		return "", nil
	}
}

func (a *Adapter) convertWebhookPayload(
	ctx context.Context,
	op dto.OPCode,
	eventType dto.EventType,
	rawData json.RawMessage,
) (*event.Event, error) {
	if a.converter == nil {
		return nil, nil
	}
	return a.converter.Convert(ctx, op, eventType, rawData)
}

func parseWebhookPayload(body []byte) (*dto.Payload, json.RawMessage, error) {
	payload := &dto.Payload{}
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.UseNumber()
	if err := decoder.Decode(payload); err != nil {
		return nil, nil, err
	}
	envelope := struct {
		Data json.RawMessage `json:"d,omitempty"`
	}{}
	if err := json.Unmarshal(body, &envelope); err != nil {
		return nil, nil, err
	}
	return payload, envelope.Data, nil
}

func buildWebhookValidationResponse(secret string, raw json.RawMessage) (*dto.WHValidationResponse, error) {
	data := &dto.WHValidationRequest{}
	if err := json.Unmarshal(raw, data); err != nil {
		return nil, err
	}

	header := http.Header{}
	header.Set(signature.HeaderTimestamp, data.EventTs)
	signed, err := signature.Generate(secret, header, []byte(data.PlainToken))
	if err != nil {
		return nil, err
	}

	return &dto.WHValidationResponse{
		PlainToken: data.PlainToken,
		Signature:  signed,
	}, nil
}

func webhookHeartbeatACKSeq(payload *dto.Payload) uint32 {
	if payload == nil {
		return 0
	}
	if seq, ok := payloadSequence(payload); ok && seq >= 0 && seq <= math.MaxUint32 {
		return uint32(seq)
	}
	switch value := payload.Data.(type) {
	case json.Number:
		if number, err := value.Int64(); err == nil && number >= 0 && number <= math.MaxUint32 {
			return uint32(number)
		}
	case float64:
		if value >= 0 && value <= math.MaxUint32 {
			return uint32(value)
		}
	case int:
		if value >= 0 {
			return uint32(value)
		}
	case int64:
		if value >= 0 && value <= math.MaxUint32 {
			return uint32(value)
		}
	case uint64:
		if value <= math.MaxUint32 {
			return uint32(value)
		}
	case uint32:
		return value
	}
	return 0
}
