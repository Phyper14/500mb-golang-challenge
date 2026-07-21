// Package config loads runtime configuration from environment
// variables, with production-sane defaults so the binary runs correctly
// inside the challenge's docker-compose without any extra wiring.
package config

import (
	"fmt"
	"os"
	"strconv"
	"time"
)

// Config holds every environment-tunable setting of the service.
type Config struct {
	// ListenAddr is the address the HTTP server binds to.
	ListenAddr string
	// InstanceID identifies this replica in the X-Instance-Id header. If
	// unset, it falls back to the OS hostname (stable and unique per
	// container in the compose topology).
	InstanceID string
	// RedisAddr is the "host:port" of the Redis instance.
	RedisAddr string
	// RedisPoolSize is the maximum number of Redis connections per
	// process. Kept small deliberately: with 3 API replicas sharing a
	// single Redis capped at ~60-80MB, an unbounded pool risks exhausting
	// Redis' own connection/memory overhead under load.
	RedisPoolSize int
	// MaxPointsPerDevice bounds the retained history per device (oldest
	// points are trimmed), keeping Redis memory bounded under sustained
	// or endurance load.
	MaxPointsPerDevice int64
	// ShutdownTimeout is the grace period given to in-flight requests
	// when the process receives SIGTERM (contract: <= 10s).
	ShutdownTimeout time.Duration
	// ReadTimeout/WriteTimeout/IdleTimeout tune the HTTP server per
	// net/http.Server semantics.
	ReadTimeout  time.Duration
	WriteTimeout time.Duration
	IdleTimeout  time.Duration
}

// Load builds a Config from environment variables, applying defaults for
// anything unset. It never panics; malformed numeric/duration values
// fall back to their default and are reported via the returned error so
// main() can decide whether to log-and-continue or fail fast.
func Load() (Config, error) {
	cfg := Config{
		ListenAddr:         getEnv("LISTEN_ADDR", ":8000"),
		InstanceID:         getEnv("INSTANCE_ID", ""),
		RedisAddr:          getEnv("REDIS_ADDR", "localhost:6379"),
		RedisPoolSize:      10,
		MaxPointsPerDevice: 5000,
		ShutdownTimeout:    10 * time.Second,
		ReadTimeout:        5 * time.Second,
		WriteTimeout:       10 * time.Second,
		IdleTimeout:        60 * time.Second,
	}

	if cfg.InstanceID == "" {
		hostname, err := os.Hostname()
		if err != nil {
			hostname = "unknown"
		}
		cfg.InstanceID = hostname
	}

	var errs []error

	if v, ok, err := getEnvInt("REDIS_POOL_SIZE"); err != nil {
		errs = append(errs, err)
	} else if ok {
		cfg.RedisPoolSize = v
	}

	if v, ok, err := getEnvInt64("MAX_POINTS_PER_DEVICE"); err != nil {
		errs = append(errs, err)
	} else if ok {
		cfg.MaxPointsPerDevice = v
	}

	if v, ok, err := getEnvDuration("SHUTDOWN_TIMEOUT"); err != nil {
		errs = append(errs, err)
	} else if ok {
		cfg.ShutdownTimeout = v
	}

	if len(errs) > 0 {
		return cfg, fmt.Errorf("config: %d invalid value(s), first: %w", len(errs), errs[0])
	}
	return cfg, nil
}

func getEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func getEnvInt(key string) (int, bool, error) {
	raw := os.Getenv(key)
	if raw == "" {
		return 0, false, nil
	}
	v, err := strconv.Atoi(raw)
	if err != nil {
		return 0, false, fmt.Errorf("%s=%q: %w", key, raw, err)
	}
	return v, true, nil
}

func getEnvInt64(key string) (int64, bool, error) {
	raw := os.Getenv(key)
	if raw == "" {
		return 0, false, nil
	}
	v, err := strconv.ParseInt(raw, 10, 64)
	if err != nil {
		return 0, false, fmt.Errorf("%s=%q: %w", key, raw, err)
	}
	return v, true, nil
}

func getEnvDuration(key string) (time.Duration, bool, error) {
	raw := os.Getenv(key)
	if raw == "" {
		return 0, false, nil
	}
	v, err := time.ParseDuration(raw)
	if err != nil {
		return 0, false, fmt.Errorf("%s=%q: %w", key, raw, err)
	}
	return v, true, nil
}
