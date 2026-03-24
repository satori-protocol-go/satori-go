package client

import (
	"net/url"
	"strconv"
	"strings"
	"time"
)

type CustomOption func(*customOptions)

type customOptions struct {
	config          APIConfig
	protocolFactory ProtocolFactory
	apiInfo         *APIInfo
}

func WithCustomConfig(config APIConfig) CustomOption {
	return func(options *customOptions) {
		if options == nil || config == nil {
			return
		}
		options.config = config
		options.apiInfo = nil
	}
}

func WithCustomProtocolFactory(factory ProtocolFactory) CustomOption {
	return func(options *customOptions) {
		if options == nil {
			return
		}
		options.protocolFactory = factory
	}
}

func WithCustomHost(host string) CustomOption {
	return func(options *customOptions) {
		if options == nil {
			return
		}
		config := options.ensureAPIInfo()
		config.Host = strings.TrimSpace(host)
	}
}

func WithCustomPort(port int) CustomOption {
	return func(options *customOptions) {
		if options == nil {
			return
		}
		config := options.ensureAPIInfo()
		config.Port = port
	}
}

func WithCustomPath(path string) CustomOption {
	return func(options *customOptions) {
		if options == nil {
			return
		}
		config := options.ensureAPIInfo()
		config.Path = strings.TrimSpace(path)
	}
}

func WithCustomVersion(version string) CustomOption {
	return func(options *customOptions) {
		if options == nil {
			return
		}
		config := options.ensureAPIInfo()
		config.Version = strings.TrimSpace(version)
	}
}

func WithCustomToken(token string) CustomOption {
	return func(options *customOptions) {
		if options == nil {
			return
		}
		config := options.ensureAPIInfo()
		config.Token = token
	}
}

func WithCustomSecure(secure bool) CustomOption {
	return func(options *customOptions) {
		if options == nil {
			return
		}
		config := options.ensureAPIInfo()
		config.Secure = secure
	}
}

func WithCustomTimeout(timeout time.Duration) CustomOption {
	return func(options *customOptions) {
		if options == nil {
			return
		}
		config := options.ensureAPIInfo()
		config.Timeout = timeout
	}
}

func (options *customOptions) ensureAPIInfo() *APIInfo {
	if options.apiInfo != nil {
		return options.apiInfo
	}
	info := apiInfoFromConfig(options.config)
	options.apiInfo = &info
	options.config = options.apiInfo
	return options.apiInfo
}

func apiInfoFromConfig(config APIConfig) APIInfo {
	switch typed := config.(type) {
	case APIInfo:
		result := typed
		result.normalize()
		return result
	case *APIInfo:
		if typed != nil {
			result := *typed
			result.normalize()
			return result
		}
	}

	result := APIInfo{}
	if config != nil {
		result.Token = config.TokenValue()
		result.Timeout = config.TimeoutValue()
	}
	base := strings.TrimSpace(apiBaseValue(config))
	if base == "" {
		result.normalize()
		return result
	}

	parsed, err := url.Parse(base)
	if err != nil {
		result.normalize()
		return result
	}

	if host := strings.TrimSpace(parsed.Hostname()); host != "" {
		result.Host = host
	}
	if portRaw := strings.TrimSpace(parsed.Port()); portRaw != "" {
		if parsedPort, parseErr := strconv.Atoi(portRaw); parseErr == nil {
			result.Port = parsedPort
		}
	}
	result.Secure = strings.EqualFold(parsed.Scheme, "https")
	assignPathAndVersion(&result, parsed.Path)
	result.normalize()
	return result
}

func apiBaseValue(config APIConfig) string {
	if config == nil {
		return ""
	}
	return config.APIBase()
}

func assignPathAndVersion(info *APIInfo, rawPath string) {
	if info == nil {
		return
	}
	path := strings.Trim(strings.TrimSpace(rawPath), "/")
	if path == "" {
		return
	}
	segments := strings.Split(path, "/")
	if len(segments) == 0 {
		return
	}
	info.Version = segments[len(segments)-1]
	if len(segments) == 1 {
		info.Path = ""
		return
	}
	info.Path = "/" + strings.Join(segments[:len(segments)-1], "/")
}
