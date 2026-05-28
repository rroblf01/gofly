package logger

import (
	"context"
	"log/slog"
	"os"
	"time"
)

var log *slog.Logger

func Init() {
	log = slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	}))
}

func InitDebug() {
	log = slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelDebug,
	}))
}

type ctxKey string

const reqIDKey ctxKey = "request_id"

func WithRequestID(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, reqIDKey, id)
}

func RequestID(ctx context.Context) string {
	if id, ok := ctx.Value(reqIDKey).(string); ok {
		return id
	}
	return ""
}

type LogFields map[string]any

func Info(msg string, fields LogFields) {
	if log != nil {
		log.Info(msg, slogFields(fields)...)
	}
}

func Debug(msg string, fields LogFields) {
	if log != nil {
		log.Debug(msg, slogFields(fields)...)
	}
}

func Warn(msg string, fields LogFields) {
	if log != nil {
		log.Warn(msg, slogFields(fields)...)
	}
}

func Error(msg string, fields LogFields) {
	if log != nil {
		log.Error(msg, slogFields(fields)...)
	}
}

func slogFields(fields LogFields) []any {
	attrs := make([]any, 0, len(fields)*2)
	for k, v := range fields {
		attrs = append(attrs, k, v)
	}
	return attrs
}

func LogAccess(start time.Time, method, path string, status int, dur time.Duration, upstream string) {
	if log != nil {
		log.Info("access",
			slog.String("method", method),
			slog.String("path", path),
			slog.Int("status", status),
			slog.Duration("duration", dur),
			slog.String("upstream", upstream),
			slog.String("ts", start.Format(time.RFC3339)),
		)
	}
}
