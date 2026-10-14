// Package main is the entry point of the team-task-nexus HTTP server.
package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/redis/go-redis/v9"

	"github.com/andrewhigh08/team-task-nexus/internal/app"
	"github.com/andrewhigh08/team-task-nexus/internal/config"
	"github.com/andrewhigh08/team-task-nexus/internal/pkg/logger"
)

// version is the build version, injected with -ldflags "-X main.version=...".
var version = "dev"

func main() {
	os.Exit(run())
}

// run loads the configuration, builds the application and serves it until SIGINT or SIGTERM.
func run() int {
	cfg, err := config.Load()
	if err != nil {
		fmt.Fprintf(os.Stderr, "team-task-nexus: invalid configuration: %v\n", err)
		return 1
	}

	logCfg := logger.DefaultConfig()
	logCfg.Level = cfg.Log.Level
	logCfg.Format = cfg.Log.Format
	log := logger.New(logCfg)
	// go-redis logs through one logger for the whole process, so it is set here once rather than
	// in app.New.
	redis.SetLogger(logger.NewRedisLogger(log.WithComponent("redis")))

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	log.Info("starting team-task-nexus", "version", version)
	application, err := app.New(ctx, cfg, log, version)
	if err != nil {
		log.Error("failed to start team-task-nexus", "error", err)
		return 1
	}
	if err := application.Run(ctx); err != nil {
		log.Error("team-task-nexus stopped with an error", "error", err)
		return 1
	}
	log.Info("team-task-nexus stopped")
	return 0
}
