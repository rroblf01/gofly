package main

import (
	"flag"
	"fmt"
	"net"
	"os"
	"time"

	"github.com/rroblf01/gofly/internal/config"
	"github.com/rroblf01/gofly/internal/logger"
	"github.com/rroblf01/gofly/internal/metrics"
	"github.com/rroblf01/gofly/internal/server"
)

// version is set at build time via -ldflags "-X main.version=...". It defaults
// to "dev" for plain `go build`/`go run`.
var version = "dev"

func main() {
	configPath := flag.String("config", "", "path to config file")
	port := flag.Int("port", 0, "override port (optional)")
	debug := flag.Bool("debug", false, "enable debug logging")
	showVersion := flag.Bool("version", false, "show version")
	root := flag.String("root", "", "serve static files from this directory (configless mode)")
	healthCheck := flag.Bool("health", false, "perform health check against running server")
	testConfig := flag.Bool("t", false, "test configuration: load and validate, then exit")
	flag.Parse()

	if *showVersion {
		fmt.Println("gofly " + version)
		return
	}

	if *healthCheck {
		healthPort := *port
		if healthPort == 0 {
			healthPort = 80
		}
		conn, err := net.DialTimeout("tcp", fmt.Sprintf("127.0.0.1:%d", healthPort), 3*time.Second)
		if err != nil {
			os.Exit(1)
		}
		conn.Close()
		return
	}

	logger.Init()
	if *debug {
		logger.InitDebug()
	}
	metrics.SetVersion(version)

	var cfg config.Config
	var path string

	if *root != "" {
		cfg = config.Default()
		if *port > 0 {
			cfg.Port = *port
		}
		cfg.Routes = []config.Route{
			{
				Path:      "/",
				StaticDir: *root,
			},
		}
		path = ""
		logger.Info("configless mode", logger.LogFields{
			"root": *root,
			"port": cfg.Port,
		})
	} else {
		path = *configPath
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
			os.Exit(1)
		}

		if *port > 0 {
			cfg.Port = *port
		}
	}

	if *testConfig {
		// config.Load already validated the schema in file mode; additionally
		// build the routing table so route conflicts are caught here rather
		// than panicking at startup.
		if err := server.CheckRoutes(cfg); err != nil {
			logger.Error("configuration test failed", logger.LogFields{"error": err.Error()})
			os.Exit(1)
		}
		if path != "" {
			fmt.Printf("gofly: the configuration file %s syntax is ok\n", path)
		} else {
			fmt.Println("gofly: configuration is ok")
		}
		fmt.Println("gofly: configuration test is successful")
		return
	}

	if err := server.Run(cfg, path); err != nil {
		logger.Error("server error", logger.LogFields{
			"error": err.Error(),
		})
		os.Exit(1)
	}
}
