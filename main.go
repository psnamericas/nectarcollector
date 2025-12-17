package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"log/slog"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"nectarcollector/capture"
	"nectarcollector/config"
	"nectarcollector/monitoring"

	"gopkg.in/natefinch/lumberjack.v2"
)

const (
	appName    = "NectarCollector"
	appVersion = "1.0.0"
)

func main() {
	// Parse command-line flags
	configPath := flag.String("config", "", "Path to configuration file")
	debug := flag.Bool("debug", false, "Enable debug logging")
	version := flag.Bool("version", false, "Show version and exit")
	flag.Parse()

	// Handle version flag
	if *version {
		fmt.Printf("%s v%s\n", appName, appVersion)
		os.Exit(0)
	}

	// Validate config path
	if *configPath == "" {
		log.Fatal("Error: -config flag is required")
	}

	// Load configuration
	cfg, err := config.Load(*configPath)
	if err != nil {
		log.Fatalf("Failed to load configuration: %v", err)
	}

	// Setup logging
	logger := setupLogging(cfg, *debug)
	logger.Info("Starting NectarCollector",
		"version", appVersion,
		"instance", cfg.App.InstanceID,
		"config", *configPath)

	// Create context with signal handling
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	// Create capture manager
	manager := capture.NewManager(cfg, *configPath, logger)

	// Start capture channels first (creates HTTP channels we need for routing)
	if err := manager.Start(ctx); err != nil {
		logger.Error("Failed to start capture manager", "error", err)
		os.Exit(1)
	}

	// Start monitoring server (registers HTTP channels for routing)
	monServer := monitoring.NewServer(&cfg.Monitoring, manager, cfg.Logging.BasePath, logger)
	if err := monServer.Start(); err != nil {
		logger.Error("Failed to start monitoring server", "error", err)
		os.Exit(1)
	}

	logger.Info("NectarCollector started successfully",
		"instance", cfg.App.InstanceID,
		"monitoring_port", cfg.Monitoring.Port)

	// Wait for shutdown signal
	sig := <-sigChan
	logger.Info("Received shutdown signal", "signal", sig.String())
	cancel()

	// Graceful shutdown
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer shutdownCancel()

	logger.Info("Shutting down gracefully...")

	// Stop monitoring server
	if err := monServer.Stop(shutdownCtx); err != nil {
		logger.Warn("Error stopping monitoring server", "error", err)
	}

	// Stop capture manager
	done := make(chan struct{})
	go func() {
		manager.Stop()
		close(done)
	}()

	select {
	case <-done:
		logger.Info("Shutdown complete")
	case <-shutdownCtx.Done():
		logger.Warn("Shutdown timed out, forcing exit")
	}

	logger.Info("NectarCollector stopped")
}

// setupLogging configures logging with optional file rotation
func setupLogging(cfg *config.Config, debug bool) *slog.Logger {
	// Determine log level
	level := slog.LevelInfo
	if debug {
		level = slog.LevelDebug
	} else {
		switch cfg.Logging.Level {
		case "debug":
			level = slog.LevelDebug
		case "info":
			level = slog.LevelInfo
		case "warn":
			level = slog.LevelWarn
		case "error":
			level = slog.LevelError
		}
	}

	opts := &slog.HandlerOptions{
		Level: level,
	}

	var handler slog.Handler

	// If log base path is configured, write to rotating log file
	if cfg.Logging.BasePath != "" {
		// Create log directory if it doesn't exist
		if err := os.MkdirAll(cfg.Logging.BasePath, 0755); err != nil {
			log.Printf("Warning: failed to create log directory: %v", err)
			handler = slog.NewTextHandler(os.Stdout, opts)
		} else {
			logPath := filepath.Join(cfg.Logging.BasePath, "nectarcollector.log")
			writer := &lumberjack.Logger{
				Filename:   logPath,
				MaxSize:    cfg.Logging.MaxSizeMB,
				MaxBackups: cfg.Logging.MaxBackups,
				Compress:   cfg.Logging.Compress,
			}
			handler = slog.NewJSONHandler(writer, opts)
		}
	} else {
		// Log to stdout
		handler = slog.NewTextHandler(os.Stdout, opts)
	}

	return slog.New(handler)
}
