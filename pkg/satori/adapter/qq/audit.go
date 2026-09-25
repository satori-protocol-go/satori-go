package qq

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/satori-protocol-go/satori-go/pkg/satori/logging"
)

const defaultAuditWait = 60 * time.Second
const maxAuditEntries = 1024 // Local correlation capacity, not a QQ quota.

type auditKey struct{ appID, auditID string }
type auditResult struct {
	Passed    bool
	MessageID string
	ChannelID string
	GuildID   string
	Reason    string
}
type auditEntry struct {
	done    chan struct{}
	result  *auditResult
	expires time.Time
	waiters int
}

// The existing correlation store holds pending waiters and bounded early
// results together. Expiry is swept during access; no maintenance goroutine.
func (a *Adapter) auditEntryLocked(key auditKey, now time.Time) (*auditEntry, error) {
	for id, entry := range a.audits {
		if entry.waiters == 0 && !now.Before(entry.expires) {
			delete(a.audits, id)
		}
	}
	if entry := a.audits[key]; entry != nil {
		return entry, nil
	}
	if len(a.audits) >= maxAuditEntries {
		var oldest auditKey
		var expiry time.Time
		for id, entry := range a.audits {
			if entry.waiters == 0 && (expiry.IsZero() || entry.expires.Before(expiry)) {
				oldest = id
				expiry = entry.expires
			}
		}
		if expiry.IsZero() {
			return nil, errors.New("local QQ audit correlation capacity reached")
		}
		delete(a.audits, oldest)
	}
	entry := &auditEntry{done: make(chan struct{}), expires: now.Add(defaultAuditWait)}
	a.audits[key] = entry
	return entry, nil
}

func (a *Adapter) captureAuditResult(appID, eventType string, raw json.RawMessage) {
	var data struct {
		AuditID   string `json:"audit_id"`
		MessageID string `json:"message_id"`
		ChannelID string `json:"channel_id"`
		GuildID   string `json:"guild_id"`
		Reason    string `json:"reject_reason"`
	}
	if json.Unmarshal(raw, &data) != nil || data.AuditID == "" {
		return
	}
	if eventType != "MESSAGE_AUDIT_PASS" && eventType != "MESSAGE_AUDIT_REJECT" {
		return
	}
	a.auditMu.Lock()
	entry := a.audits[auditKey{auditID: data.AuditID}]
	if entry != nil && entry.result == nil {
		entry.result = &auditResult{Passed: eventType == "MESSAGE_AUDIT_PASS", MessageID: data.MessageID, ChannelID: data.ChannelID, GuildID: data.GuildID, Reason: data.Reason}
		entry.expires = time.Now().Add(defaultAuditWait)
		close(entry.done)
	}
	a.auditMu.Unlock()
	a.log(context.Background(), logging.LevelInfo, fmt.Sprintf("QQ audit result app_id=%q audit_id=%q outcome=%q", appID, data.AuditID, eventType))
	// The original audit event is still published by the common event path.
}

func (a *Adapter) waitAuditResult(ctx context.Context, appID, auditID string, timeout time.Duration) (auditResult, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return auditResult{}, err
	}
	if err := a.eventContext.Err(); err != nil {
		return auditResult{}, err
	}
	if timeout <= 0 {
		timeout = defaultAuditWait
	}
	a.auditMu.Lock()
	entry, err := a.auditEntryLocked(auditKey{auditID: auditID}, time.Now())
	if err == nil {
		entry.waiters++
	}
	a.auditMu.Unlock()
	if err != nil {
		return auditResult{}, err
	}
	defer func() { a.auditMu.Lock(); entry.waiters--; a.auditMu.Unlock() }()
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	select {
	case <-entry.done:
		a.auditMu.Lock()
		result := *entry.result
		a.auditMu.Unlock()
		return result, nil
	case <-ctx.Done():
		return auditResult{}, ctx.Err()
	case <-a.eventContext.Done():
		return auditResult{}, a.eventContext.Err()
	case <-timer.C:
		return auditResult{}, context.DeadlineExceeded
	}
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
