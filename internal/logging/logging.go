package logging

import (
	"os"

	"github.com/distr-sh/distr-controller-contrib/internal/config"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

// New writes info and debug to stdout and everything from warn upwards to stderr. The Distr Docker
// controller forwards both streams of a container and marks stderr lines as errors, so the split carries
// the severity into the deployment logs.
func New(level zapcore.Level, format config.LogFormat) *zap.Logger {
	encoderConfig := zap.NewProductionEncoderConfig()
	encoderConfig.TimeKey = "time"
	encoderConfig.EncodeTime = zapcore.ISO8601TimeEncoder
	encoderConfig.EncodeDuration = zapcore.StringDurationEncoder

	var encoder zapcore.Encoder
	if format == config.LogFormatJSON {
		encoder = zapcore.NewJSONEncoder(encoderConfig)
	} else {
		encoderConfig.EncodeLevel = zapcore.CapitalLevelEncoder
		encoderConfig.ConsoleSeparator = " "
		encoder = zapcore.NewConsoleEncoder(encoderConfig)
	}

	low := zap.LevelEnablerFunc(func(l zapcore.Level) bool { return l >= level && l < zapcore.WarnLevel })
	high := zap.LevelEnablerFunc(func(l zapcore.Level) bool { return l >= level && l >= zapcore.WarnLevel })

	return zap.New(zapcore.NewTee(
		zapcore.NewCore(encoder, zapcore.Lock(os.Stdout), low),
		zapcore.NewCore(encoder, zapcore.Lock(os.Stderr), high),
	))
}
