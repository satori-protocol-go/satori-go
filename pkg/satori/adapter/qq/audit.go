package qq

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

const defaultAuditWait = 60 * time.Second

func (a *Adapter) captureAuditResult(raw json.RawMessage) {
	if len(bytes.TrimSpace(raw)) == 0 {
		return
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	payload := map[string]any{}
	if err := decoder.Decode(&payload); err != nil {
		return
	}

	auditID := strings.TrimSpace(anyString(payload["audit_id"]))
	if auditID == "" {
		return
	}
	messageID := strings.TrimSpace(anyString(payload["message_id"]))
	if messageID == "" {
		messageID = strings.TrimSpace(anyString(payload["id"]))
	}

	a.auditMu.Lock()
	waiters := a.auditWaiters[auditID]
	delete(a.auditWaiters, auditID)
	a.auditMu.Unlock()

	for _, ch := range waiters {
		select {
		case ch <- messageID:
		default:
		}
		close(ch)
	}
}

func (a *Adapter) waitAuditMessageID(ctx context.Context, auditID string, timeout time.Duration) (string, bool) {
	auditID = strings.TrimSpace(auditID)
	if auditID == "" {
		return "", false
	}
	if timeout <= 0 {
		timeout = defaultAuditWait
	}
	if ctx == nil {
		ctx = context.Background()
	}

	ch := make(chan string, 1)
	a.auditMu.Lock()
	a.auditWaiters[auditID] = append(a.auditWaiters[auditID], ch)
	a.auditMu.Unlock()

	timer := time.NewTimer(timeout)
	defer timer.Stop()

	select {
	case value, ok := <-ch:
		if !ok || strings.TrimSpace(value) == "" {
			return "", false
		}
		return strings.TrimSpace(value), true
	case <-ctx.Done():
	case <-timer.C:
	}

	a.auditMu.Lock()
	waiters := a.auditWaiters[auditID]
	filtered := make([]chan string, 0, len(waiters))
	for _, waiter := range waiters {
		if waiter != ch {
			filtered = append(filtered, waiter)
		}
	}
	if len(filtered) == 0 {
		delete(a.auditWaiters, auditID)
	} else {
		a.auditWaiters[auditID] = filtered
	}
	a.auditMu.Unlock()
	return "", false
}

func anyString(raw any) string {
	if raw == nil {
		return ""
	}
	switch typed := raw.(type) {
	case string:
		return typed
	case json.Number:
		return typed.String()
	default:
		return fmt.Sprint(raw)
	}
}
