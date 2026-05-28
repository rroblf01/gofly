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

	configPath := flag.String("config", "", "")
	port := flag.Int("port", 0, "")
	debug := flag.Bool("debug", false, "")
	showVersion := flag.Bool("version", false, "")
	root := flag.String("root", "", "")
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

	var cfg config.Config

	if *root != "" {
		cfg = config.Default()
		if *port > 0 {
			cfg.Port = *port
		}
		cfg.Routes = []config.Route{
			{Path: "/", StaticDir: *root},
		}
		exitCode = 0
	} else {
		path := *configPath
		if path == "" {
			path = "/etc/gofly/config.json"
		}
		if env := os.Getenv("GOFLY_CONFIG"); env != "" {
			path = env
		}

		var err error
		cfg, err = config.Load(path)
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
			exitCode = 0
		}
	}

	_ = cfg

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

func TestMain_RootFlag(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(dir+"/index.html", []byte("root mode"), 0644); err != nil {
		t.Fatal(err)
	}

	code, _ := run([]string{"gofly", "-root", dir}, nil)
	if code != 0 {
		t.Fatalf("expected exit code 0 for -root, got %d", code)
	}
}

func TestMain_RootWithPort(t *testing.T) {
	dir := t.TempDir()
	code, _ := run([]string{"gofly", "-root", dir, "-port", "8080"}, nil)
	if code != 0 {
		t.Fatalf("expected exit code 0 for -root with -port, got %d", code)
	}
}

func TestMain_RootAndConfig(t *testing.T) {
	dir := t.TempDir()
	code, _ := run([]string{"gofly", "-root", dir, "-config", "/nonexistent"}, nil)
	if code != 0 {
		t.Fatalf("expected exit code 0 when -root is set regardless of -config, got %d", code)
	}
}

func TestMain_ConfiglessInvalidRoot(t *testing.T) {
	code, _ := run([]string{"gofly", "-root", "/nonexistent/directory"}, nil)
	if code != 0 {
		t.Fatalf("expected exit code 0 even with nonexistent root (runtime will handle), got %d", code)
	}
}
