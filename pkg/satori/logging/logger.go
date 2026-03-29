package logging

import (
	"context"
	"fmt"
	"log"
	"os"
	"strings"
)

type Level string

const (
	LevelDebug Level = "debug"
	LevelInfo  Level = "info"
	LevelWarn  Level = "warn"
	LevelError Level = "error"
)

type Logger interface {
	Log(ctx context.Context, level Level, v ...any)
}

type stdLogger struct {
	inner *log.Logger
}

type NopLogger struct{}

func NewStdLogger() Logger {
	return &stdLogger{
		inner: log.New(os.Stderr, "", log.LstdFlags),
	}
}

func (NopLogger) Log(context.Context, Level, ...any) {}

func (l *stdLogger) Log(_ context.Context, level Level, v ...any) {
	if l == nil || l.inner == nil {
		return
	}

	var builder strings.Builder
	builder.WriteString("level=")
	builder.WriteString(string(level))
	if len(v) > 0 {
		builder.WriteString(" msg=")
		builder.WriteString(fmt.Sprint(v...))
	}
	l.inner.Print(builder.String())
}
