package qq

import (
	"context"
	"io"
	"net/http"
	"strings"

	botgodto "github.com/WindowsSov8forUs/botgo-plus/dto"
	qqwebhook "github.com/satori-protocol-go/satori-go/pkg/satori/adapter/qq/webhook"
	"github.com/satori-protocol-go/satori-go/pkg/satori/model/event"
	satoriserver "github.com/satori-protocol-go/satori-go/pkg/satori/server"
)

func (a *Adapter) RootRoutes() []satoriserver.RootRoute {
	return qqwebhook.BuildRootRoutes(
		normalizeWebhookPaths(a.path),
		http.HandlerFunc(a.handleWebhookRequest),
	)
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

	payload, err := qqwebhook.ParsePayload(body)
	if err != nil {
		http.Error(w, "invalid payload", http.StatusBadRequest)
		return
	}

	appID := strings.TrimSpace(request.Header.Get("X-Bot-Appid"))
	if appID == "" || appID != a.appID {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	if payload.Op == botgodto.HTTPCallbackValidation {
		response, validationErr := qqwebhook.BuildValidationResponse(a.cfg.Secret, payload.Data)
		if validationErr != nil {
			http.Error(w, validationErr.Error(), http.StatusBadRequest)
			return
		}
		writeJSON(w, response)
		return
	}

	if !a.skipSignatureCheck {
		if verifyErr := qqwebhook.VerifySignature(a.cfg.Secret, request.Header, body); verifyErr != nil {
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
	payload *qqwebhook.Payload,
) (*event.Event, error) {
	if a.converter == nil {
		return nil, nil
	}
	return a.converter.Convert(ctx, payload)
}
