package stream

import (
	"context"
	"testing"
	"time"

	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newTestRedisManager(t *testing.T, ttl time.Duration, prefix string) (*RedisStreamManager, *miniredis.Miniredis) {
	t.Helper()
	s, err := miniredis.Run()
	require.NoError(t, err)
	t.Cleanup(s.Close)

	mgr, err := NewRedisStreamManager(s.Addr(), "", "", 0, prefix, ttl)
	require.NoError(t, err)
	t.Cleanup(func() { _ = mgr.Close() })
	return mgr, s
}

func TestNewRedisStreamManager_Defaults(t *testing.T) {
	s, err := miniredis.Run()
	require.NoError(t, err)
	t.Cleanup(s.Close)

	mgr, err := NewRedisStreamManager(s.Addr(), "", "", 0, "", 0)
	require.NoError(t, err)
	t.Cleanup(func() { _ = mgr.Close() })

	// Empty prefix / zero TTL fall back to documented defaults.
	assert.Equal(t, "stream:events", mgr.prefix)
	assert.Equal(t, 24*time.Hour, mgr.ttl)
}

func TestNewRedisStreamManager_ConnectionFailure(t *testing.T) {
	mgr, err := NewRedisStreamManager("127.0.0.1:1", "", "", 0, "", time.Minute)
	require.Error(t, err)
	assert.Nil(t, mgr)
}

func TestRedisStreamManager_BuildKey(t *testing.T) {
	mgr, _ := newTestRedisManager(t, time.Hour, "myprefix")
	assert.Equal(t, "myprefix:sess:msg", mgr.buildKey("sess", "msg"))
}

func TestRedisStreamManager_AppendAndGet(t *testing.T) {
	mgr, _ := newTestRedisManager(t, time.Hour, "p")
	ctx := context.Background()

	require.NoError(t, mgr.AppendEvent(ctx, "s1", "m1", interfaces.StreamEvent{ID: "e1", Content: "a"}))
	require.NoError(t, mgr.AppendEvent(ctx, "s1", "m1", interfaces.StreamEvent{ID: "e2", Content: "b"}))

	events, next, err := mgr.GetEvents(ctx, "s1", "m1", 0)
	require.NoError(t, err)
	require.Len(t, events, 2)
	assert.Equal(t, "e1", events[0].ID)
	assert.Equal(t, "b", events[1].Content)
	assert.Equal(t, 2, next)
	assert.False(t, events[0].Timestamp.IsZero())
}

func TestRedisStreamManager_GetEvents_MissingKey(t *testing.T) {
	mgr, _ := newTestRedisManager(t, time.Hour, "p")

	events, next, err := mgr.GetEvents(context.Background(), "nope", "nope", 3)
	require.NoError(t, err)
	assert.Empty(t, events)
	assert.Equal(t, 3, next)
}

func TestRedisStreamManager_IncrementalRead(t *testing.T) {
	mgr, _ := newTestRedisManager(t, time.Hour, "p")
	ctx := context.Background()

	for _, id := range []string{"e1", "e2", "e3"} {
		require.NoError(t, mgr.AppendEvent(ctx, "s1", "m1", interfaces.StreamEvent{ID: id}))
	}

	events, next, err := mgr.GetEvents(ctx, "s1", "m1", 2)
	require.NoError(t, err)
	require.Len(t, events, 1)
	assert.Equal(t, "e3", events[0].ID)
	assert.Equal(t, 3, next)

	// Reading past the end returns nothing and keeps the offset.
	events, next, err = mgr.GetEvents(ctx, "s1", "m1", 3)
	require.NoError(t, err)
	assert.Empty(t, events)
	assert.Equal(t, 3, next)
}

func TestRedisStreamManager_SetsTTL(t *testing.T) {
	mgr, s := newTestRedisManager(t, 30*time.Minute, "p")
	ctx := context.Background()

	require.NoError(t, mgr.AppendEvent(ctx, "s1", "m1", interfaces.StreamEvent{ID: "e1"}))

	ttl := s.TTL(mgr.buildKey("s1", "m1"))
	assert.Positive(t, ttl)
	assert.LessOrEqual(t, ttl, 30*time.Minute)
}

func TestRedisStreamManager_SkipsCorruptEntries(t *testing.T) {
	mgr, s := newTestRedisManager(t, time.Hour, "p")
	ctx := context.Background()

	require.NoError(t, mgr.AppendEvent(ctx, "s1", "m1", interfaces.StreamEvent{ID: "good"}))

	// Inject a non-JSON entry directly; GetEvents should skip it but still
	// return the valid ones (and account for it in the offset).
	key := mgr.buildKey("s1", "m1")
	_, err := s.Lpush(key, "not-json")
	require.NoError(t, err)

	events, next, err := mgr.GetEvents(ctx, "s1", "m1", 0)
	require.NoError(t, err)
	require.Len(t, events, 1)
	assert.Equal(t, "good", events[0].ID)
	assert.Equal(t, 2, next)
}

func TestRedisStreamManager_GetEventsError(t *testing.T) {
	mgr, s := newTestRedisManager(t, time.Hour, "p")
	ctx := context.Background()

	// A WRONGTYPE against a string key surfaces as an error (not redis.Nil).
	key := mgr.buildKey("s1", "m1")
	s.Set(key, "iamastring")

	events, _, err := mgr.GetEvents(ctx, "s1", "m1", 0)
	require.Error(t, err)
	assert.Nil(t, events)
}

func TestRedisStreamManager_Close(t *testing.T) {
	s, err := miniredis.Run()
	require.NoError(t, err)
	t.Cleanup(s.Close)

	mgr, err := NewRedisStreamManager(s.Addr(), "", "", 0, "p", time.Hour)
	require.NoError(t, err)
	require.NoError(t, mgr.Close())

	// After Close, the client is unusable.
	err = mgr.client.Ping(context.Background()).Err()
	assert.ErrorIs(t, err, redis.ErrClosed)
}

func TestRedisStreamManager_ImplementsInterface(t *testing.T) {
	var _ interfaces.StreamManager = (*RedisStreamManager)(nil)
}
