package applog

import (
	"os"
	"strings"
	"time"

	"github.com/rs/zerolog"
)

type Logger struct {
	zerolog.Logger
}

func New(level, format string) *Logger {
	zerolog.TimeFieldFormat = time.RFC3339Nano
	var zl zerolog.Logger
	if format == "console" {
		zl = zerolog.New(zerolog.ConsoleWriter{Out: os.Stderr, TimeFormat: time.RFC3339}).
			With().Timestamp().Caller().Logger()
	} else {
		zl = zerolog.New(os.Stderr).With().Timestamp().Logger()
	}
	switch strings.ToLower(level) {
	case "debug":
		zl = zl.Level(zerolog.DebugLevel)
	case "info":
		zl = zl.Level(zerolog.InfoLevel)
	case "warn":
		zl = zl.Level(zerolog.WarnLevel)
	case "error":
		zl = zl.Level(zerolog.ErrorLevel)
	default:
		zl = zl.Level(zerolog.InfoLevel)
	}
	return &Logger{Logger: zl}
}
