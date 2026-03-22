package main

import (
	"context"
	"os"
	"os/signal"
	"syscall"
	"time"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"

	"github.com/hemantkpr/mailmate/internal/config"
	"github.com/hemantkpr/mailmate/internal/server"
)

var version = "dev"

func main() {
	// Load configuration
	cfg, err := config.Load()
	if err != nil {
		panic("failed to load config: " + err.Error())
	}

	// Initialize logger
	logger := initLogger(cfg.Log.Level, cfg.Log.Format)
	defer logger.Sync()

	logger.Info("starting MailMate", zap.String("version", version))

	// Create server
	srv, err := server.New(cfg, logger)
	if err != nil {
		logger.Fatal("failed to create server", zap.Error(err))
	}

	// Start server in goroutine
	go func() {
		if err := srv.Start(); err != nil {
			logger.Fatal("server failed", zap.Error(err))
		}
	}()

	// Wait for interrupt signal
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	sig := <-quit

	logger.Info("shutting down", zap.String("signal", sig.String()))

	// Graceful shutdown with timeout
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if err := srv.Shutdown(ctx); err != nil {
		logger.Error("shutdown error", zap.Error(err))
	}

	logger.Info("server stopped")
}

func initLogger(level, format string) *zap.Logger {
	var zapLevel zapcore.Level
	switch level {
	case "debug":
		zapLevel = zap.DebugLevel
	case "warn":
		zapLevel = zap.WarnLevel
	case "error":
		zapLevel = zap.ErrorLevel
	default:
		zapLevel = zap.InfoLevel
	}

	var cfg zap.Config
	if format == "console" {
		cfg = zap.NewDevelopmentConfig()
	} else {
		cfg = zap.NewProductionConfig()
	}
	cfg.Level = zap.NewAtomicLevelAt(zapLevel)

	logger, err := cfg.Build()
	if err != nil {
		panic("failed to create logger: " + err.Error())
	}
	return logger
}
