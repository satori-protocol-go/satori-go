package qq

import (
	"encoding/json"
	"net/http"
	"sort"
	"strings"

	botgodto "github.com/WindowsSov8forUs/botgo-plus/dto"
	"github.com/satori-protocol-go/satori-go/pkg/satori/model/login"
	"github.com/satori-protocol-go/satori-go/pkg/satori/model/user"
)

func (a *Adapter) apiBaseURL() string {
	if a.cfg.Sandbox {
		return qqSandboxAPIBaseURL
	}
	return qqAPIBaseURL
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

func firstNonEmpty(items ...string) string {
	for _, item := range items {
		if strings.TrimSpace(item) != "" {
			return item
		}
	}
	return ""
}

func copyStrings(items []string) []string {
	if len(items) == 0 {
		return nil
	}
	result := make([]string, len(items))
	copy(result, items)
	return result
}

func valueOrDefaultFeatures(values []string, defaults []string) []string {
	if len(values) == 0 {
		return copyStrings(defaults)
	}
	return copyStrings(values)
}

func copyUser(item *user.User) *user.User {
	if item == nil {
		return nil
	}
	cloned := *item
	return &cloned
}

func cloneLogin(item *login.Login) *login.Login {
	if item == nil {
		return nil
	}
	cloned := *item
	cloned.User = copyUser(item.User)
	cloned.Features = copyStrings(item.Features)
	return &cloned
}

func platformByEventType(eventType string) string {
	switch eventType {
	case string(botgodto.EventGroupAtMessageCreate),
		string(botgodto.EventGroupAddRobot),
		string(botgodto.EventGroupDelRobot),
		string(botgodto.EventGroupMsgReject),
		string(botgodto.EventGroupMsgReceive),
		string(botgodto.EventC2CMessageCreate),
		string(botgodto.EventFriendAdd),
		string(botgodto.EventFriendDel),
		string(botgodto.EventC2CMsgReceive),
		string(botgodto.EventC2CMsgReject):
		return "qq"
	default:
		return "qqguild"
	}
}
