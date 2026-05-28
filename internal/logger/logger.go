package logger

import (
	"context"
	"log/slog"
	"os"
	"time"
)

var Log *slog.Logger

func Init() {
	Log = slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	}))
}

func InitDebug() {
	Log = slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
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
	if Log != nil {
		Log.Info(msg, slogFields(fields)...)
	}
}

func Debug(msg string, fields LogFields) {
	if Log != nil {
		Log.Debug(msg, slogFields(fields)...)
	}
}

func Warn(msg string, fields LogFields) {
	if Log != nil {
		Log.Warn(msg, slogFields(fields)...)
	}
}

func Error(msg string, fields LogFields) {
	if Log != nil {
		Log.Error(msg, slogFields(fields)...)
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
	if Log != nil {
		Log.Info("access",
			slog.String("method", method),
			slog.String("path", path),
			slog.Int("status", status),
			slog.Duration("duration", dur),
			slog.String("upstream", upstream),
			slog.String("ts", start.Format(time.RFC3339)),
		)
	}
}
