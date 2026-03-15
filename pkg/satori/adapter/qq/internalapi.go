package qq

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	satoriserver "github.com/satori-protocol-go/satori-go/pkg/satori/server"
)

func (a *Adapter) HandleInternal(
	request satoriserver.Request[map[string]any],
	path string,
) (*satoriserver.Response, error) {
	path = strings.TrimSpace(path)
	if !strings.HasPrefix(path, "_api") {
		return nil, satoriserver.NotFound("internal path is not supported")
	}

	action := strings.TrimPrefix(path, "_api")
	action = strings.TrimPrefix(action, "/")
	if action == "" {
		return nil, satoriserver.BadRequest("internal api action is required")
	}

	method := http.MethodGet
	ctx := context.Background()
	if request.Origin != nil {
		method = request.Origin.Method
		ctx = request.Origin.Context()
	}
	if strings.TrimSpace(method) == "" {
		method = http.MethodGet
	}

	params := request.Params
	if params == nil {
		params = map[string]any{}
	}

	body, contentType, status, err := a.callRawAPI(ctx, method, action, params)
	if err != nil {
		return nil, err
	}
	response := satoriserver.NewResponse(status, body)
	if contentType != "" {
		response.Header.Set("Content-Type", contentType)
	}
	return response, nil
}

func (a *Adapter) callRawAPI(
	ctx context.Context,
	method string,
	action string,
	params map[string]any,
) ([]byte, string, int, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	method = strings.ToUpper(strings.TrimSpace(method))
	if method == "" {
		method = http.MethodGet
	}

	targetURL := strings.TrimRight(a.apiBaseURL(), "/") + "/" + strings.TrimLeft(action, "/")
	var body io.Reader
	if method == http.MethodGet || method == http.MethodDelete {
		query := url.Values{}
		for key, value := range params {
			switch typed := value.(type) {
			case nil:
				continue
			case []string:
				for _, item := range typed {
					query.Add(key, item)
				}
			case []any:
				for _, item := range typed {
					query.Add(key, fmt.Sprint(item))
				}
			default:
				query.Set(key, fmt.Sprint(value))
			}
		}
		if encoded := query.Encode(); encoded != "" {
			targetURL += "?" + encoded
		}
	} else {
		payload, err := json.Marshal(params)
		if err != nil {
			return nil, "", 0, err
		}
		body = bytes.NewReader(payload)
	}

	req, err := http.NewRequestWithContext(ctx, method, targetURL, body)
	if err != nil {
		return nil, "", 0, err
	}

	accessToken, err := a.accessToken(ctx)
	if err != nil {
		return nil, "", 0, err
	}
	req.Header.Set("Authorization", "QQBot "+accessToken)
	req.Header.Set("X-Union-Appid", a.appID)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := a.httpClient.Do(req)
	if err != nil {
		return nil, "", 0, err
	}
	defer resp.Body.Close()

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, "", 0, err
	}
	if resp.StatusCode >= 400 {
		return nil, "", 0, satoriserver.NewActionFailed(resp.StatusCode, string(data), nil)
	}
	return data, resp.Header.Get("Content-Type"), resp.StatusCode, nil
}

func (a *Adapter) accessToken(ctx context.Context) (string, error) {
	if a.token == nil {
		return "", errors.New("token is not configured")
	}
	tokenValue := strings.TrimSpace(a.token.GetAccessToken())
	if tokenValue != "" {
		return tokenValue, nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	_ = a.token.InitToken(ctx)
	tokenValue = strings.TrimSpace(a.token.GetAccessToken())
	if tokenValue == "" {
		return "", errors.New("qq access token is empty")
	}
	return tokenValue, nil
}
