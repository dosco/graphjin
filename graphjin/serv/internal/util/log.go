package util

import (
	"os"
	"time"

	"github.com/thessem/zap-prettyconsole"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

// shortTimeEncoder encodes time in HH:MM:SS format for cleaner console output
func shortTimeEncoder(t time.Time, enc zapcore.PrimitiveArrayEncoder) {
	enc.AppendString(t.Format("15:04:05"))
}

// NewLogger creates a new zap logger instance
// json - if true logs are in json format
func NewLogger(json bool) *zap.Logger {
	return NewLoggerWithOutput(json, os.Stdout)
}

// NewLoggerWithOutput creates a new zap logger instance with a custom output
// json - if true logs are in json format
// output - the output writer (e.g., os.Stdout, os.Stderr)
func NewLoggerWithOutput(json bool, output zapcore.WriteSyncer) *zap.Logger {
	econf := zapcore.EncoderConfig{
		MessageKey:     "msg",
		LevelKey:       "level",
		TimeKey:        "time",
		NameKey:        "logger",
		EncodeLevel:    zapcore.LowercaseLevelEncoder,
		EncodeTime:     zapcore.ISO8601TimeEncoder,
		EncodeDuration: zapcore.StringDurationEncoder,
	}

	var core zapcore.Core

	if json {
		core = zapcore.NewCore(zapcore.NewJSONEncoder(econf), output, zap.DebugLevel)
	} else {
		// Use prettyconsole for human-readable key=value output
		pcfg := prettyconsole.NewEncoderConfig()
		pcfg.EncodeTime = shortTimeEncoder
		core = zapcore.NewCore(prettyconsole.NewEncoder(pcfg), output, zap.DebugLevel)
	}
	return zap.New(core)
}
