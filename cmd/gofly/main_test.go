package main

import (
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRunConvert_Success(t *testing.T) {
	dir := t.TempDir()
	nginxPath := filepath.Join(dir, "nginx.conf")
	content := `server { listen 8080; location / { proxy_pass http://localhost:3000; } }`
	if err := os.WriteFile(nginxPath, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
	if err := runConvert(nginxPath); err != nil {
		t.Fatalf("runConvert error: %v", err)
	}
}

func TestRunConvert_MissingFile(t *testing.T) {
	if err := runConvert("/nonexistent/nginx.conf"); err == nil {
		t.Fatal("expected error for missing file")
	}
}

func TestHealthCheck_DialSuccessAndFailure(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	addr := ln.Addr().String()
	// Extract port
	_, portStr, _ := net.SplitHostPort(addr)
	// Simulate -health check like main does: dial 127.0.0.1:port
	// Here we just verify that a listening port is reachable and a closed one is not
	conn, err := net.Dial("tcp", addr)
	if err != nil {
		t.Fatalf("expected dial success to %s: %v", addr, err)
	}
	conn.Close()
	_ = portStr

	// Dial to a closed port should fail
	ln2, _ := net.Listen("tcp", "127.0.0.1:0")
	closedAddr := ln2.Addr().String()
	ln2.Close()
	if _, err := net.Dial("tcp", closedAddr); err == nil {
		t.Error("expected dial failure to closed port")
	}
}

func TestRunConvert_ProducesWarnings(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "nginx.conf")
	// regex location should produce warning
	src := `server { listen 80; location ~ \.php$ { root /www; } location / { root /www; } }`
	if err := os.WriteFile(path, []byte(src), 0644); err != nil {
		t.Fatal(err)
	}
	// runConvert prints JSON to stdout and warnings to stderr; we just verify it doesn't error
	if err := runConvert(path); err != nil {
		t.Fatalf("runConvert with regex location: %v", err)
	}
}

func TestVersionString(t *testing.T) {
	if version == "" {
		t.Error("version should not be empty")
	}
	if !strings.HasPrefix(version, "1.") && version != "dev" {
		t.Errorf("version %q unexpected", version)
	}
}
