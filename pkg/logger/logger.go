package logger

import (
	"os"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

const (
	logDir  = "logs"
	logFile = "logs/app.log"
)

// New creates a *zap.Logger that writes structured JSON to both stdout and
// a log file. The log file is created if it doesn't exist and truncated on
// every call (clearing previous runs). level should be one of zap's level
// strings: debug, info, warn, error, dpanic, panic, fatal.
func New(level string) *zap.Logger {
	zapLevel := parseLevel(level)

	if err := os.MkdirAll(logDir, 0o755); err != nil {
		panic("logger: failed to create logs directory: " + err.Error())
	}

	file, err := os.Create(logFile)
	if err != nil {
		panic("logger: failed to create log file: " + err.Error())
	}

	encCfg := zap.NewProductionEncoderConfig()
	encCfg.TimeKey = "ts"
	encCfg.EncodeTime = zapcore.ISO8601TimeEncoder
	encCfg.EncodeLevel = zapcore.LowercaseLevelEncoder

	jsonEncoder := zapcore.NewJSONEncoder(encCfg)
	consoleEncoder := zapcore.NewConsoleEncoder(encCfg)

	core := zapcore.NewTee(
		zapcore.NewCore(jsonEncoder, zapcore.AddSync(file), zapLevel),
		zapcore.NewCore(consoleEncoder, zapcore.AddSync(os.Stdout), zapLevel),
	)

	return zap.New(core, zap.AddCaller(), zap.AddStacktrace(zapcore.ErrorLevel))
}

func parseLevel(level string) zapcore.Level {
	var l zapcore.Level
	if err := l.UnmarshalText([]byte(level)); err != nil {
		return zapcore.InfoLevel
	}
	return l
}
