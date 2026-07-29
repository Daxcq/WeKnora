package stream

import (
	"testing"

	"github.com/alicebob/miniredis/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewStreamManager_DefaultsToMemory(t *testing.T) {
	t.Setenv("STREAM_MANAGER_TYPE", "")

	mgr, err := NewStreamManager()
	require.NoError(t, err)
	assert.IsType(t, &MemoryStreamManager{}, mgr)
}

func TestNewStreamManager_UnknownTypeFallsBackToMemory(t *testing.T) {
	t.Setenv("STREAM_MANAGER_TYPE", "bogus")

	mgr, err := NewStreamManager()
	require.NoError(t, err)
	assert.IsType(t, &MemoryStreamManager{}, mgr)
}

func TestNewStreamManager_Redis(t *testing.T) {
	s, err := miniredis.Run()
	require.NoError(t, err)
	t.Cleanup(s.Close)

	t.Setenv("STREAM_MANAGER_TYPE", TypeRedis)
	t.Setenv("REDIS_ADDR", s.Addr())
	t.Setenv("REDIS_USERNAME", "")
	t.Setenv("REDIS_PASSWORD", "")
	t.Setenv("REDIS_DB", "")
	t.Setenv("REDIS_PREFIX", "")

	mgr, err := NewStreamManager()
	require.NoError(t, err)
	rm, ok := mgr.(*RedisStreamManager)
	require.True(t, ok)
	t.Cleanup(func() { _ = rm.Close() })

	// An unparseable REDIS_DB defaults to DB 0 rather than erroring.
	assert.Equal(t, "stream:events", rm.prefix)
}

func TestNewStreamManager_RedisUnreachable(t *testing.T) {
	t.Setenv("STREAM_MANAGER_TYPE", TypeRedis)
	// Port 1 is not listening: the ping fails fast with connection refused.
	t.Setenv("REDIS_ADDR", "127.0.0.1:1")

	mgr, err := NewStreamManager()
	require.Error(t, err)
	assert.Nil(t, mgr)
}
