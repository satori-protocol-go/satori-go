package qq

import (
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/WindowsSov8forUs/botgo-plus/errs"
	"github.com/satori-protocol-go/satori-go/pkg/satori/logging"
	"github.com/satori-protocol-go/satori-go/pkg/satori/server"
)

// describeActionLog reads the existing result without consuming streams or changing the response.
func describeActionLog(action, platform, selfID string, status, code int, trace string, elapsed time.Duration, result any, cause error) string {
	identity := fmt.Sprintf("QQ API call %s for %s bot %s", logging.SafeText(action), eventLogID(platform), eventLogID(selfID))
	if cause == nil && status >= 200 && status < 300 {
		return fmt.Sprintf("%s completed in %d ms (HTTP %d).", identity, elapsed.Milliseconds(), status)
	}
	reason := logging.ErrorText(cause)
	var apiError *errs.APIError
	if errors.As(cause, &apiError) && apiError.Message != "" {
		reason = logging.SafeText(apiError.Message)
	}
	var body []byte
	switch response := result.(type) {
	case *server.Response:
		if response != nil {
			body = response.Body
		}
	case server.Response:
		body = response.Body
	}
	var partial struct {
		Error    string            `json:"error"`
		Messages []json.RawMessage `json:"messages"`
	}
	if status >= 400 && json.Unmarshal(body, &partial) == nil && partial.Error != "" {
		reason = logging.ErrorText(errors.New(partial.Error))
	}
	text := fmt.Sprintf("%s ended with HTTP %d after %d ms.", identity, status, elapsed.Milliseconds())
	if cause != nil || status >= 400 {
		if reason == "" {
			reason = "no error details were provided"
		}
		text = fmt.Sprintf("%s failed: %s (HTTP %d).", identity, reason, status)
		if len(partial.Messages) > 0 {
			text = fmt.Sprintf("%s sent %d messages before failing: %s (HTTP %d).", identity, len(partial.Messages), reason, status)
		}
	}
	if code != 0 {
		text += fmt.Sprintf(" QQ error code: %d.", code)
	}
	if trace != "" {
		text += " Trace ID: " + logging.SafeText(trace) + "."
	}
	return text
}
