package qq

import (
	"errors"
	"time"

	botgoopenapi "github.com/WindowsSov8forUs/botgo-plus/openapi"
	botgotoken "github.com/WindowsSov8forUs/botgo-plus/token"
)

func createOpenAPIClients(
	token *botgotoken.Token,
	sandbox bool,
	timeout time.Duration,
) (OpenAPI, OpenAPI, error) {
	v1Impl, ok := botgoopenapi.VersionMapping[botgoopenapi.APIv1]
	if !ok || v1Impl == nil {
		return nil, nil, errors.New("botgo-plus openapi v1 is not registered")
	}
	v2Impl, ok := botgoopenapi.VersionMapping[botgoopenapi.APIv2]
	if !ok || v2Impl == nil {
		return nil, nil, errors.New("botgo-plus openapi v2 is not registered")
	}

	v1 := v1Impl.Setup(token, sandbox)
	v2 := v2Impl.Setup(token, sandbox)
	if timeout > 0 {
		v1 = v1.WithTimeout(timeout)
		v2 = v2.WithTimeout(timeout)
	}
	return v1, v2, nil
}
