package logger

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"testing"
	"time"
)

func TestInit(t *testing.T) {
	var buf bytes.Buffer
	log = slog.New(slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: slog.LevelInfo}))
	defer func() { log = nil }()

	Info("hello", LogFields{"key": "val"})

	if buf.Len() == 0 {
		t.Fatal("expected log output")
	}

	var result map[string]any
	if err := json.Unmarshal(buf.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if result["msg"] != "hello" {
		t.Errorf("expected msg 'hello', got %v", result["msg"])
	}
	if result["key"] != "val" {
		t.Errorf("expected key 'val', got %v", result["key"])
	}
	if result["level"] != "INFO" {
		t.Errorf("expected level 'INFO', got %v", result["level"])
	}
}

func TestInitDebug(t *testing.T) {
	var buf bytes.Buffer
	log = slog.New(slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug}))
	defer func() { log = nil }()

	Debug("debug msg", nil)
	if buf.Len() == 0 {
		t.Fatal("expected debug log output, got none")
	}

	var result map[string]any
	if err := json.Unmarshal(buf.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if result["msg"] != "debug msg" {
		t.Errorf("expected msg 'debug msg', got %v", result["msg"])
	}
	if result["level"] != "DEBUG" {
		t.Errorf("expected level 'DEBUG', got %v", result["level"])
	}
}

func TestNilSafety(t *testing.T) {
	log = nil

	Info("safe", nil)
	Error("safe", nil)
	Debug("safe", nil)
	Warn("safe", nil)
	LogAccess(time.Now(), "GET", "/", 200, time.Second, "up")
	WithRequestID(context.Background(), "x")
	RequestID(context.Background())
}

func TestRequestID(t *testing.T) {
	ctx := WithRequestID(context.Background(), "abc-123")
	if got := RequestID(ctx); got != "abc-123" {
		t.Errorf("expected 'abc-123', got %q", got)
	}
	if got := RequestID(context.Background()); got != "" {
		t.Errorf("expected empty string, got %q", got)
	}
}

func TestLogAccess(t *testing.T) {
	var buf bytes.Buffer
	log = slog.New(slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: slog.LevelInfo}))
	defer func() { log = nil }()

	start := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	LogAccess(start, "POST", "/api/test", 201, 500*time.Millisecond, "localhost")

	if buf.Len() == 0 {
		t.Fatal("expected log output")
	}

	var result map[string]any
	if err := json.Unmarshal(buf.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if result["msg"] != "access" {
		t.Errorf("expected msg 'access', got %v", result["msg"])
	}
	if result["method"] != "POST" {
		t.Errorf("expected method 'POST', got %v", result["method"])
	}
	if result["path"] != "/api/test" {
		t.Errorf("expected path '/api/test', got %v", result["path"])
	}
	if result["upstream"] != "localhost" {
		t.Errorf("expected upstream 'localhost', got %v", result["upstream"])
	}
}

func TestLogFields(t *testing.T) {
	var buf bytes.Buffer
	log = slog.New(slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: slog.LevelInfo}))
	defer func() { log = nil }()

	Info("info msg", LogFields{"user": "alice", "count": 42})

	if buf.Len() == 0 {
		t.Fatal("expected log output")
	}

	var result map[string]any
	if err := json.Unmarshal(buf.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if result["msg"] != "info msg" {
		t.Errorf("expected msg 'info msg', got %v", result["msg"])
	}
	if result["user"] != "alice" {
		t.Errorf("expected user 'alice', got %v", result["user"])
	}
	if c, ok := result["count"].(float64); !ok || c != 42 {
		t.Errorf("expected count 42, got %v", result["count"])
	}

	buf.Reset()
	Error("error msg", LogFields{"err": "timeout"})

	if buf.Len() == 0 {
		t.Fatal("expected log output")
	}

	if err := json.Unmarshal(buf.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if result["msg"] != "error msg" {
		t.Errorf("expected msg 'error msg', got %v", result["msg"])
	}
	if result["err"] != "timeout" {
		t.Errorf("expected err 'timeout', got %v", result["err"])
	}
	if result["level"] != "ERROR" {
		t.Errorf("expected level 'ERROR', got %v", result["level"])
	}
}
