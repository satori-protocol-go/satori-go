package qq

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"

	"github.com/WindowsSov8forUs/botgo-plus/dto"
	"github.com/WindowsSov8forUs/botgo-plus/interaction/signature"
	"github.com/go-chi/chi/v5"
	"github.com/satori-protocol-go/satori-go/pkg/satori/model/event"
)

func (a *Adapter) RegisterRootRoutes(router chi.Router) {
	if router == nil {
		return
	}
	handler := http.HandlerFunc(a.handleWebhookRequest)
	for _, path := range normalizeWebhookPaths(a.path) {
		router.Handle(path, handler)
	}
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

	body, err := io.ReadAll(request.Body)
	if err != nil {
		http.Error(w, "failed to read request body", http.StatusBadRequest)
		return
	}

	payload, err := parseWebhookPayload(body)
	if err != nil {
		http.Error(w, "invalid payload", http.StatusBadRequest)
		return
	}

	appID := strings.TrimSpace(request.Header.Get("X-Bot-Appid"))
	if appID == "" || appID != a.appID {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	if payload.Op == dto.HTTPCallbackValidation {
		response, validationErr := buildWebhookValidationResponse(a.cfg.Secret, payload.Data)
		if validationErr != nil {
			http.Error(w, validationErr.Error(), http.StatusBadRequest)
			return
		}
		writeJSON(w, response)
		return
	}

	if !a.skipSignatureCheck {
		if verifyErr := verifyWebhookSignature(a.cfg.Secret, request.Header, body); verifyErr != nil {
			http.Error(w, "invalid signature", http.StatusUnauthorized)
			return
		}
	}

	evt, convertErr := a.convertWebhookPayload(request.Context(), payload)
	if convertErr != nil {
		http.Error(w, convertErr.Error(), http.StatusBadRequest)
		return
	}
	if evt != nil {
		a.pushEvent(evt)
	}
	w.WriteHeader(http.StatusOK)
}

func (a *Adapter) convertWebhookPayload(
	ctx context.Context,
	payload *webhookPayload,
) (*event.Event, error) {
	if a.converter == nil {
		return nil, nil
	}
	return a.converter.Convert(ctx, payload.Op, payload.Type, payload.Data)
}

type webhookPayload struct {
	Op   dto.OPCode      `json:"op"`
	Type dto.EventType   `json:"t,omitempty"`
	Seq  int64           `json:"s,omitempty"`
	ID   string          `json:"id,omitempty"`
	Data json.RawMessage `json:"d,omitempty"`
}

func parseWebhookPayload(body []byte) (*webhookPayload, error) {
	payload := &webhookPayload{}
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.UseNumber()
	if err := decoder.Decode(payload); err != nil {
		return nil, err
	}
	return payload, nil
}

func verifyWebhookSignature(secret string, header http.Header, body []byte) error {
	passed, err := signature.Verify(secret, header, body)
	if err != nil {
		return err
	}
	if !passed {
		return fmt.Errorf("invalid signature")
	}
	return nil
}

func buildWebhookValidationResponse(secret string, raw json.RawMessage) (*dto.WHValidationResponse, error) {
	data := &dto.WHValidationRequest{}
	if err := json.Unmarshal(raw, data); err != nil {
		return nil, err
	}

	privateKey := ed25519.NewKeyFromSeed([]byte(deriveWebhookSeed(secret)))
	message := []byte(data.EventTs + data.PlainToken)
	signature := hex.EncodeToString(ed25519.Sign(privateKey, message))

	return &dto.WHValidationResponse{
		PlainToken: data.PlainToken,
		Signature:  signature,
	}, nil
}

func deriveWebhookSeed(secret string) string {
	if secret == "" {
		return strings.Repeat("0", ed25519.SeedSize)
	}
	seed := secret
	for len(seed) < ed25519.SeedSize {
		seed += secret
	}
	return seed[:ed25519.SeedSize]
}
