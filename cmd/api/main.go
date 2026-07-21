// Command api is the entry point of the 500MB Club telemetry service. It
// wires configuration, the Redis-backed store and the HTTP handlers, and
// runs the server with graceful shutdown on SIGTERM/SIGINT.
package main

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"

	"github.com/redis/go-redis/v9"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"

	"github.com/Phyper14/500mb-golang-challenge/internal/config"
	"github.com/Phyper14/500mb-golang-challenge/internal/httpapi"
	"github.com/Phyper14/500mb-golang-challenge/internal/storage/rediskv"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run() error {
	logger, err := newLogger()
	if err != nil {
		return fmt.Errorf("new logger: %w", err)
	}
	defer func() { _ = logger.Sync() }()

	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	redisClient := redis.NewClient(&redis.Options{
		Addr:     cfg.RedisAddr,
		PoolSize: cfg.RedisPoolSize,
	})
	store := rediskv.New(redisClient, cfg.MaxPointsPerDevice)
	defer func() {
		if closeErr := store.Close(); closeErr != nil {
			logger.Warn("close storage", zap.Error(closeErr))
		}
	}()

	srv := httpapi.NewServer(store, cfg.InstanceID)
	mux := http.NewServeMux()
	srv.Routes(mux)

	httpServer := &http.Server{
		Addr:         cfg.ListenAddr,
		Handler:      httpapi.InstrumentMetrics(mux),
		ReadTimeout:  cfg.ReadTimeout,
		WriteTimeout: cfg.WriteTimeout,
		IdleTimeout:  cfg.IdleTimeout,
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT)
	defer stop()

	serveErr := make(chan error, 1)
	go func() {
		logger.Info("listening",
			zap.String("addr", cfg.ListenAddr),
			zap.String("instance_id", cfg.InstanceID),
			zap.String("redis_addr", cfg.RedisAddr),
		)
		serveErr <- httpServer.ListenAndServe()
	}()

	select {
	case err := <-serveErr:
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			return fmt.Errorf("listen and serve: %w", err)
		}
		return nil
	case <-ctx.Done():
		logger.Info("shutdown signal received, draining in-flight requests",
			zap.Duration("timeout", cfg.ShutdownTimeout))

		shutdownCtx, cancel := context.WithTimeout(context.Background(), cfg.ShutdownTimeout)
		defer cancel()

		if err := httpServer.Shutdown(shutdownCtx); err != nil {
			return fmt.Errorf("graceful shutdown: %w", err)
		}
		logger.Info("shutdown complete")
		return nil
	}
}

func newLogger() (*zap.Logger, error) {
	cfg := zap.NewProductionConfig()
	cfg.EncoderConfig.TimeKey = "ts"
	cfg.EncoderConfig.EncodeTime = zapcore.RFC3339NanoTimeEncoder
	return cfg.Build()
}
