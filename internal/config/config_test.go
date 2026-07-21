package config

import (
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func clearEnv(t *testing.T, keys ...string) {
	t.Helper()
	for _, k := range keys {
		original, existed := os.LookupEnv(k)
		require.NoError(t, os.Unsetenv(k))
		t.Cleanup(func() {
			if existed {
				_ = os.Setenv(k, original)
			}
		})
	}
}

var allKeys = []string{
	"LISTEN_ADDR", "INSTANCE_ID", "REDIS_ADDR", "REDIS_POOL_SIZE",
	"MAX_POINTS_PER_DEVICE", "SHUTDOWN_TIMEOUT",
}

func TestLoad_Defaults(t *testing.T) {
	clearEnv(t, allKeys...)

	cfg, err := Load()
	require.NoError(t, err)

	assert.Equal(t, ":8000", cfg.ListenAddr)
	assert.Equal(t, "localhost:6379", cfg.RedisAddr)
	assert.Equal(t, 10, cfg.RedisPoolSize)
	assert.Equal(t, int64(5000), cfg.MaxPointsPerDevice)
	assert.Equal(t, 10*time.Second, cfg.ShutdownTimeout)
	assert.NotEmpty(t, cfg.InstanceID, "should fall back to hostname when INSTANCE_ID unset")
}

func TestLoad_OverridesFromEnv(t *testing.T) {
	clearEnv(t, allKeys...)

	t.Setenv("LISTEN_ADDR", ":9000")
	t.Setenv("INSTANCE_ID", "api-1")
	t.Setenv("REDIS_ADDR", "redis:6379")
	t.Setenv("REDIS_POOL_SIZE", "25")
	t.Setenv("MAX_POINTS_PER_DEVICE", "1000")
	t.Setenv("SHUTDOWN_TIMEOUT", "5s")

	cfg, err := Load()
	require.NoError(t, err)

	assert.Equal(t, ":9000", cfg.ListenAddr)
	assert.Equal(t, "api-1", cfg.InstanceID)
	assert.Equal(t, "redis:6379", cfg.RedisAddr)
	assert.Equal(t, 25, cfg.RedisPoolSize)
	assert.Equal(t, int64(1000), cfg.MaxPointsPerDevice)
	assert.Equal(t, 5*time.Second, cfg.ShutdownTimeout)
}

func TestLoad_InvalidNumericValue(t *testing.T) {
	clearEnv(t, allKeys...)
	t.Setenv("REDIS_POOL_SIZE", "not-a-number")

	_, err := Load()
	assert.Error(t, err)
}

func TestLoad_InvalidDuration(t *testing.T) {
	clearEnv(t, allKeys...)
	t.Setenv("SHUTDOWN_TIMEOUT", "not-a-duration")

	_, err := Load()
	assert.Error(t, err)
}
