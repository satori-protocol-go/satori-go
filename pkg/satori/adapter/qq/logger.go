package qq

import (
	"context"
	"fmt"

	"github.com/satori-protocol-go/satori-go/pkg/satori/logging"
)

func (a *Adapter) RegisterLogger(logger logging.Logger) {
	if logger == nil {
		logger = logging.NopLogger{}
	}
	a.mu.Lock()
	a.logger = logger
	a.mu.Unlock()
}

func (a *Adapter) Logger() logging.Logger {
	a.mu.RLock()
	defer a.mu.RUnlock()
	if a.logger == nil {
		return logging.NopLogger{}
	}
	return a.logger
}

func (a *Adapter) log(ctx context.Context, level logging.Level, v ...any) {
	logger := a.Logger()
	if logger == nil {
		return
	}
	logger.Log(ctx, level, v...)
}

func (a *Adapter) logf(level logging.Level, format string, args ...any) {
	a.log(context.Background(), level, fmt.Sprintf(format, args...))
}
