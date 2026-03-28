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

type Field struct {
	Key   string
	Value any
}

type Logger interface {
	Log(ctx context.Context, level Level, message string, fields ...Field)
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

func (NopLogger) Log(context.Context, Level, string, ...Field) {}

func (l *stdLogger) Log(_ context.Context, level Level, message string, fields ...Field) {
	if l == nil || l.inner == nil {
		return
	}

	var builder strings.Builder
	builder.WriteString("level=")
	builder.WriteString(string(level))
	if message != "" {
		builder.WriteString(" msg=")
		builder.WriteString(message)
	}
	for _, field := range fields {
		key := strings.TrimSpace(field.Key)
		if key == "" {
			continue
		}
		builder.WriteString(" ")
		builder.WriteString(key)
		builder.WriteString("=")
		builder.WriteString(fmt.Sprint(field.Value))
	}
	l.inner.Print(builder.String())
}
