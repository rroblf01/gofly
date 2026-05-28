package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/rroblf01/gofly/internal/config"
	"github.com/rroblf01/gofly/internal/logger"
	"github.com/rroblf01/gofly/internal/server"
)

func main() {
	configPath := flag.String("config", "/etc/gofly/config.json", "path to config file")
	port := flag.Int("port", 0, "override port (optional)")
	debug := flag.Bool("debug", false, "enable debug logging")
	showVersion := flag.Bool("version", false, "show version")
	flag.Parse()

	if *showVersion {
		fmt.Println("gofly v0.1.0")
		return
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
		os.Exit(1)
	}

	if *port > 0 {
		cfg.Port = *port
	}

	if err := server.Run(cfg); err != nil {
		logger.Error("server error", logger.LogFields{
			"error": err.Error(),
		})
		os.Exit(1)
	}
}
