package qq

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/satori-protocol-go/satori-go/pkg/satori/model/event"
	"net/http"
	"sort"
	"strings"

	"github.com/WindowsSov8forUs/botgo-plus/dto"
	"github.com/go-chi/chi/v5"
	"github.com/satori-protocol-go/satori-go/pkg/satori/adapter/qq/convert"
	"github.com/satori-protocol-go/satori-go/pkg/satori/logging"
)

func (a *Adapter) RegisterRootRoutes(router chi.Router) {
	if router == nil || a.wsEnabled {
		return
	}
	for _, path := range normalizeWebhookPaths(a.path) {
		router.Handle(path, http.HandlerFunc(a.handleWebhookRequest))
	}
	a.log(context.Background(), logging.LevelInfo, "Registered the QQ Webhook callback routes.")
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
	trimmed := strings.TrimSuffix(path, "/")
	if trimmed == "" {
		trimmed = "/"
	}
	result := []string{path}
	if trimmed != path {
		result = append(result, trimmed)
	}
	sort.Strings(result)
	return result
}

func (a *Adapter) handleWebhookRequest(w http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	state := a.stateByAppID(request.Header.Get("X-Bot-Appid"))
	if state == nil {
		a.log(request.Context(), logging.LevelWarn, "Rejected a QQ Webhook request for an unknown application.")
		http.Error(w, "unknown QQ application", http.StatusUnauthorized)
		return
	}
	response := &webhookResponse{ResponseWriter: w, status: http.StatusOK}
	state.webhook.ServeHTTP(response, request)
	level := logging.LevelDebug
	if response.status >= 400 {
		level = logging.LevelWarn
	}
	if response.status >= 500 {
		level = logging.LevelError
	}
	description := fmt.Sprintf("The QQ Webhook request for app %s ended with HTTP %d.", logging.SafeText(state.appID), response.status)
	if response.status >= 200 && response.status < 300 {
		description = fmt.Sprintf("Handled the QQ Webhook request for app %s (HTTP %d).", logging.SafeText(state.appID), response.status)
	}
	a.log(request.Context(), level, description)
}

// Both native transports enter this conversion boundary after SDK validation.
func (a *Adapter) acceptPayload(ctx context.Context, state *appState, payload *dto.WSPayload) (resultErr error) {
	defer func() {
		if resultErr != nil {
			appID := appIDFromContext(ctx)
			if state != nil {
				appID = state.appID
			}
			description := "Failed to handle a QQ event"
			if appID != "" {
				description += " for app " + logging.SafeText(appID)
			}
			a.log(ctx, logging.LevelError, description+": "+logging.ErrorText(resultErr))
		}
	}()
	if state == nil || payload == nil || a.converter == nil {
		return errors.New("invalid QQ event context")
	}
	raw := payloadDataFromEvent(payload)
	if strings.HasPrefix(string(payload.Type), "MESSAGE_AUDIT_") {
		a.captureAuditResult(state.appID, string(payload.Type), raw)
	}
	evt, err := a.converter.Convert(withAppID(ctx, state.appID), payload.OPCode, payload.Type, raw)
	if err != nil {
		return err
	}
	if evt == nil {
		return nil
	}
	if payload.Type == dto.EventInteractionCreate {
		var interaction dto.Interaction
		if err := json.Unmarshal(raw, &interaction); err != nil {
			return err
		}
		if interaction.ApplicationID != "" && interaction.ApplicationID != state.appID {
			return errors.New("QQ interaction application does not match the connection")
		}
		kind := convert.InteractionKind(&interaction)
		if (kind == 11 || kind == 12) && !a.cfg.ManualInteractionResponse {
			if _, err := state.api.AcknowledgeInteraction(ctx, interaction.ID, 0); err != nil {
				return err
			}
		}
	}
	if evt.Referrer == nil {
		evt.Referrer = map[string]any{}
	}
	evt.Referrer["app_id"] = state.appID
	if payload.EventID != "" {
		evt.Referrer["qq_event_id"] = payload.EventID
		if _, present := evt.Referrer["event_id"]; !present && evt.Type != event.EventTypeMessageCreated && evt.Type != event.EventTypeMessageDeleted {
			evt.Referrer["event_id"] = payload.EventID
		}
	}
	// _data is the complete native QQ envelope, not a reconstructed subset.
	if len(payload.RawMessage) > 0 {
		evt.Data_ = append(json.RawMessage(nil), payload.RawMessage...)
	}
	if err := a.pushEvent(ctx, evt); err != nil {
		return err
	}
	a.logEventBySource(ctx, payload.Type, evt)
	return nil
}

// The QQ callback handler only writes ordinary HTTP responses, never upgrades.
type webhookResponse struct {
	http.ResponseWriter
	status int
}

func (w *webhookResponse) WriteHeader(status int) {
	w.status = status
	w.ResponseWriter.WriteHeader(status)
}
