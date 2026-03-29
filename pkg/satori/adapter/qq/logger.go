package qq

import (
	"context"
	"fmt"

	"github.com/WindowsSov8forUs/botgo-plus"
	"github.com/satori-protocol-go/satori-go/pkg/satori/logging"
)

type qqLogger struct {
	logger logging.Logger
}

func registerQQLogger(logger logging.Logger) {
	if logger == nil {
		logger = logging.NopLogger{}
	}
	botgo.SetLogger(&qqLogger{logger: logger})
}

func (l *qqLogger) Debug(v ...interface{}) {
	l.log(context.Background(), logging.LevelDebug, v...)
}

func (l *qqLogger) Info(v ...interface{}) {
	l.log(context.Background(), logging.LevelDebug, v...)
}

func (l *qqLogger) Warn(v ...interface{}) {
	l.log(context.Background(), logging.LevelDebug, v...)
}

func (l *qqLogger) Error(v ...interface{}) {
	l.log(context.Background(), logging.LevelDebug, v...)
}

func (l *qqLogger) Debugf(format string, v ...interface{}) {
	l.log(context.Background(), logging.LevelDebug, fmt.Sprintf(format, v...))
}

func (l *qqLogger) Infof(format string, v ...interface{}) {
	l.log(context.Background(), logging.LevelDebug, fmt.Sprintf(format, v...))
}

func (l *qqLogger) Warnf(format string, v ...interface{}) {
	l.log(context.Background(), logging.LevelDebug, fmt.Sprintf(format, v...))
}

func (l *qqLogger) Errorf(format string, v ...interface{}) {
	l.log(context.Background(), logging.LevelDebug, fmt.Sprintf(format, v...))
}

func (l *qqLogger) Sync() error {
	return nil
}

func (l *qqLogger) log(ctx context.Context, level logging.Level, v ...interface{}) {
	if l == nil || l.logger == nil {
		return
	}
	message := fmt.Sprintf("[botgo-plus] %s", fmt.Sprint(v...))
	l.logger.Log(ctx, level, message)
}

func (a *Adapter) RegisterLogger(logger logging.Logger) {
	if logger == nil {
		logger = logging.NopLogger{}
	}
	registerQQLogger(logger)
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
