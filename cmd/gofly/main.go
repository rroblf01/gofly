package main

import (
	"flag"
	"fmt"
	"net"
	"os"
	"time"

	"github.com/rroblf01/gofly/internal/config"
	"github.com/rroblf01/gofly/internal/logger"
	"github.com/rroblf01/gofly/internal/server"
)

func main() {
	configPath := flag.String("config", "", "path to config file")
	port := flag.Int("port", 0, "override port (optional)")
	debug := flag.Bool("debug", false, "enable debug logging")
	showVersion := flag.Bool("version", false, "show version")
	root := flag.String("root", "", "serve static files from this directory (configless mode)")
	healthCheck := flag.Bool("health", false, "perform health check against running server")
	flag.Parse()

	if *showVersion {
		fmt.Println("gofly v0.1.0")
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

	if err := server.Run(cfg, path); err != nil {
		logger.Error("server error", logger.LogFields{
			"error": err.Error(),
		})
		os.Exit(1)
	}
}
