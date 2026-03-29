package server

import "github.com/satori-protocol-go/satori-go/pkg/satori/logging"

type LogLevel = logging.Level

const (
	LogLevelDebug LogLevel = logging.LevelDebug
	LogLevelInfo  LogLevel = logging.LevelInfo
	LogLevelWarn  LogLevel = logging.LevelWarn
	LogLevelError LogLevel = logging.LevelError
)

type Logger = logging.Logger

type NopLogger = logging.NopLogger

func NewStdLogger() Logger {
	return logging.NewStdLogger()
}
