package qq

import (
	"context"
	"errors"
	"net/http"
	"sort"
	"strings"

	"github.com/WindowsSov8forUs/botgo-plus/dto"
	"github.com/go-chi/chi/v5"
	"github.com/satori-protocol-go/satori-go/pkg/satori/logging"
)

func (a *Adapter) RegisterRootRoutes(router chi.Router) {
	if router == nil || a.wsEnabled {
		return
	}
	for _, path := range normalizeWebhookPaths(a.path) {
		router.Handle(path, http.HandlerFunc(a.handleWebhookRequest))
	}
	a.log(context.Background(), logging.LevelInfo, "QQ webhook routes registered")
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
		http.Error(w, "unknown QQ application", http.StatusUnauthorized)
		return
	}
	state.webhook.ServeHTTP(w, request)
}

// Both native transports enter this conversion boundary after SDK validation.
func (a *Adapter) acceptPayload(ctx context.Context, state *appState, payload *dto.WSPayload) error {
	if state == nil || payload == nil || a.converter == nil {
		return errors.New("invalid QQ event context")
	}
	raw := payloadDataFromEvent(payload)
	if strings.HasPrefix(string(payload.Type), "MESSAGE_AUDIT_") {
		a.captureAuditResult(raw)
	}
	evt, err := a.converter.Convert(withAppID(ctx, state.appID), payload.OPCode, payload.Type, raw)
	if err != nil {
		return err
	}
	if evt == nil {
		return nil
	}
	a.logEventBySource(payload.Type, evt)
	return a.pushEvent(ctx, evt)
}
