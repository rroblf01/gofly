package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/rroblf01/gofly/internal/config"
	"github.com/rroblf01/gofly/internal/logger"
)

// run duplicates main()'s logic but returns (exitCode, stdout) instead of
// calling os.Exit or writing directly to os.Stdout. It resets flag state so
// each call is isolated.
func run(args []string, environ map[string]string) (exitCode int, stdout string) {
	prevArgs := os.Args
	prevStdout := os.Stdout
	prevEnv := make(map[string]string)
	for k := range environ {
		prevEnv[k], _ = os.LookupEnv(k)
	}

	defer func() {
		os.Args = prevArgs
		os.Stdout = prevStdout
		for k, v := range prevEnv {
			if v != "" {
				os.Setenv(k, v)
			} else {
				os.Unsetenv(k)
			}
		}
		for k := range environ {
			if _, ok := prevEnv[k]; !ok {
				os.Unsetenv(k)
			}
		}
	}()

	for k, v := range environ {
		os.Setenv(k, v)
	}

	r, pw, _ := os.Pipe()
	os.Stdout = pw
	os.Args = args

	flag.CommandLine = flag.NewFlagSet(os.Args[0], flag.ContinueOnError)

	configPath := flag.String("config", "/etc/gofly/config.json", "")
	port := flag.Int("port", 0, "")
	debug := flag.Bool("debug", false, "")
	showVersion := flag.Bool("version", false, "")
	flag.Parse()

	if *showVersion {
		fmt.Fprintln(pw, "gofly v0.1.0")
		pw.Close()
		var buf bytes.Buffer
		io.Copy(&buf, r)
		return 0, buf.String()
	}

	logger.Init()
	if *debug {
		logger.InitDebug()
	}

	path := *configPath
	if env := os.Getenv("GOFLY_CONFIG"); env != "" {
		path = env
	}

	cfg, err := config.Load(path)
	if err != nil {
		logger.Error("failed to load config", logger.LogFields{
			"path":  path,
			"error": err.Error(),
		})
		exitCode = 1
	} else {
		if *port > 0 {
			cfg.Port = *port
		}
		_ = cfg
		exitCode = 0
	}

	pw.Close()
	var buf bytes.Buffer
	io.Copy(&buf, r)
	stdout = buf.String()

	return
}

func TestMain_VersionFlag(t *testing.T) {
	code, out := run([]string{"gofly", "-version"}, nil)
	if code != 0 {
		t.Fatalf("expected exit code 0, got %d", code)
	}
	if out != "gofly v0.1.0\n" {
		t.Fatalf("expected version output %q, got %q", "gofly v0.1.0\n", out)
	}
}

func TestMain_InvalidConfig(t *testing.T) {
	code, _ := run([]string{"gofly", "-config", "/nonexistent/gofly/config.json"}, nil)
	if code != 1 {
		t.Fatalf("expected exit code 1 for invalid config, got %d", code)
	}
}

func TestMain_ConfigFromEnv(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.json")

	valid := config.Config{
		Port: 8080,
		Routes: []config.Route{
			{Path: "/", Upstreams: []string{"http://localhost:3000"}},
		},
	}
	data, err := json.Marshal(valid)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(cfgPath, data, 0644); err != nil {
		t.Fatal(err)
	}

	code, _ := run([]string{"gofly"}, map[string]string{"GOFLY_CONFIG": cfgPath})
	if code != 0 {
		t.Fatalf("expected exit code 0 when config loaded from env, got %d", code)
	}
}
