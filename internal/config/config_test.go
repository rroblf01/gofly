package config

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestLoad_Defaults(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.json")
	data := `{"routes":[{"path":"/","upstreams":["http://localhost:8080"]}]}`
	if err := os.WriteFile(cfgPath, []byte(data), 0644); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load(cfgPath)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if cfg.Port != 80 {
		t.Errorf("default port = %d, want 80", cfg.Port)
	}
	if cfg.ReadTimeout.Duration != 30*time.Second {
		t.Errorf("default read timeout = %v, want 30s", cfg.ReadTimeout.Duration)
	}
	if cfg.WriteTimeout.Duration != 30*time.Second {
		t.Errorf("default write timeout = %v, want 30s", cfg.WriteTimeout.Duration)
	}
	if cfg.IdleTimeout.Duration != 120*time.Second {
		t.Errorf("default idle timeout = %v, want 120s", cfg.IdleTimeout.Duration)
	}
}

func TestLoad_CustomPort(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.json")
	data := `{"port":3000,"routes":[{"path":"/","upstreams":["http://localhost:8080"]}]}`
	if err := os.WriteFile(cfgPath, []byte(data), 0644); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load(cfgPath)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if cfg.Port != 3000 {
		t.Errorf("port = %d, want 3000", cfg.Port)
	}
}

func TestLoad_InvalidPort(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.json")
	data := `{"port":99999,"routes":[{"path":"/","upstreams":["http://localhost:8080"]}]}`
	if err := os.WriteFile(cfgPath, []byte(data), 0644); err != nil {
		t.Fatal(err)
	}

	_, err := Load(cfgPath)
	if err == nil {
		t.Fatal("expected error for invalid port, got nil")
	}
}

func TestLoad_MissingFile(t *testing.T) {
	_, err := Load("/nonexistent/path/config.json")
	if err == nil {
		t.Fatal("expected error for missing file, got nil")
	}
}

func TestLoad_InvalidJSON(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.json")
	data := `{invalid json}`
	if err := os.WriteFile(cfgPath, []byte(data), 0644); err != nil {
		t.Fatal(err)
	}

	_, err := Load(cfgPath)
	if err == nil {
		t.Fatal("expected error for invalid JSON, got nil")
	}
}

func TestLoad_RouteWithoutUpstreamOrStatic(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.json")
	data := `{"routes":[{"path":"/api"}]}`
	if err := os.WriteFile(cfgPath, []byte(data), 0644); err != nil {
		t.Fatal(err)
	}

	_, err := Load(cfgPath)
	if err == nil {
		t.Fatal("expected error for route without upstream or static_dir, got nil")
	}
}

func TestLoad_RouteWithStaticDir(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.json")
	data := `{"routes":[{"path":"/static","static_dir":"/var/www/html"}]}`
	if err := os.WriteFile(cfgPath, []byte(data), 0644); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load(cfgPath)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(cfg.Routes) != 1 {
		t.Errorf("routes count = %d, want 1", len(cfg.Routes))
	}
	if cfg.Routes[0].StaticDir != "/var/www/html" {
		t.Errorf("static_dir = %q, want /var/www/html", cfg.Routes[0].StaticDir)
	}
}

func TestLoad_TLSWithoutCert(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.json")
	data := `{"tls":{"enabled":true},"routes":[{"path":"/","upstreams":["http://localhost:8080"]}]}`
	if err := os.WriteFile(cfgPath, []byte(data), 0644); err != nil {
		t.Fatal(err)
	}

	_, err := Load(cfgPath)
	if err == nil {
		t.Fatal("expected error for TLS without cert_file, got nil")
	}
}

func TestLoad_TLSWithAutoCert(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.json")
	data := `{"tls":{"enabled":true,"auto_cert":true},"routes":[{"path":"/","upstreams":["http://localhost:8080"]}]}`
	if err := os.WriteFile(cfgPath, []byte(data), 0644); err != nil {
		t.Fatal(err)
	}

	_, err := Load(cfgPath)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestLoad_WithSetHeaders(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.json")
	data := `{"routes":[{"path":"/","upstreams":["http://localhost:8080"],"set_headers":{"X-Custom":"value"}}]}`
	if err := os.WriteFile(cfgPath, []byte(data), 0644); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load(cfgPath)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Routes[0].SetHeaders["X-Custom"] != "value" {
		t.Errorf("header = %q, want value", cfg.Routes[0].SetHeaders["X-Custom"])
	}
}

func TestLoad_DurationParsing(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.json")
	data := `{"read_timeout":"15s","routes":[{"path":"/","upstreams":["http://localhost:8080"]}]}`
	if err := os.WriteFile(cfgPath, []byte(data), 0644); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load(cfgPath)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.ReadTimeout.Duration != 15*time.Second {
		t.Errorf("read timeout = %v, want 15s", cfg.ReadTimeout.Duration)
	}
}

func TestDefault_Config(t *testing.T) {
	cfg := Default()
	if cfg.Port != 80 {
		t.Errorf("default port = %d, want 80", cfg.Port)
	}
}
