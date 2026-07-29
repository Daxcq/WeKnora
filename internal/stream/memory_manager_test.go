package stream

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMemoryStreamManager_GetEvents_EmptyStream(t *testing.T) {
	m := NewMemoryStreamManager()
	ctx := context.Background()

	events, next, err := m.GetEvents(ctx, "s1", "m1", 0)
	require.NoError(t, err)
	assert.Empty(t, events)
	assert.Equal(t, 0, next)
}

func TestMemoryStreamManager_AppendAndGet(t *testing.T) {
	m := NewMemoryStreamManager()
	ctx := context.Background()

	require.NoError(t, m.AppendEvent(ctx, "s1", "m1", interfaces.StreamEvent{ID: "e1", Content: "a"}))
	require.NoError(t, m.AppendEvent(ctx, "s1", "m1", interfaces.StreamEvent{ID: "e2", Content: "b"}))

	events, next, err := m.GetEvents(ctx, "s1", "m1", 0)
	require.NoError(t, err)
	require.Len(t, events, 2)
	assert.Equal(t, "e1", events[0].ID)
	assert.Equal(t, "e2", events[1].ID)
	assert.Equal(t, 2, next)

	// AppendEvent stamps a timestamp when none is provided.
	assert.False(t, events[0].Timestamp.IsZero())
}

func TestMemoryStreamManager_IncrementalReadFromOffset(t *testing.T) {
	m := NewMemoryStreamManager()
	ctx := context.Background()

	require.NoError(t, m.AppendEvent(ctx, "s1", "m1", interfaces.StreamEvent{ID: "e1"}))
	require.NoError(t, m.AppendEvent(ctx, "s1", "m1", interfaces.StreamEvent{ID: "e2"}))

	events, next, err := m.GetEvents(ctx, "s1", "m1", 1)
	require.NoError(t, err)
	require.Len(t, events, 1)
	assert.Equal(t, "e2", events[0].ID)
	assert.Equal(t, 2, next)

	// Reading at the current end yields nothing and preserves the offset.
	events, next, err = m.GetEvents(ctx, "s1", "m1", 2)
	require.NoError(t, err)
	assert.Empty(t, events)
	assert.Equal(t, 2, next)

	// An offset beyond the end behaves the same way.
	events, next, err = m.GetEvents(ctx, "s1", "m1", 99)
	require.NoError(t, err)
	assert.Empty(t, events)
	assert.Equal(t, 99, next)
}

func TestMemoryStreamManager_PreservesProvidedTimestamp(t *testing.T) {
	m := NewMemoryStreamManager()
	ctx := context.Background()
	ts := time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)

	require.NoError(t, m.AppendEvent(ctx, "s1", "m1", interfaces.StreamEvent{ID: "e1", Timestamp: ts}))

	events, _, err := m.GetEvents(ctx, "s1", "m1", 0)
	require.NoError(t, err)
	require.Len(t, events, 1)
	assert.True(t, ts.Equal(events[0].Timestamp))
}

func TestMemoryStreamManager_IsolatesSessionsAndMessages(t *testing.T) {
	m := NewMemoryStreamManager()
	ctx := context.Background()

	require.NoError(t, m.AppendEvent(ctx, "s1", "m1", interfaces.StreamEvent{ID: "a"}))
	require.NoError(t, m.AppendEvent(ctx, "s1", "m2", interfaces.StreamEvent{ID: "b"}))
	require.NoError(t, m.AppendEvent(ctx, "s2", "m1", interfaces.StreamEvent{ID: "c"}))

	for _, tc := range []struct {
		session, message, wantID string
	}{
		{"s1", "m1", "a"},
		{"s1", "m2", "b"},
		{"s2", "m1", "c"},
	} {
		events, _, err := m.GetEvents(ctx, tc.session, tc.message, 0)
		require.NoError(t, err)
		require.Len(t, events, 1)
		assert.Equal(t, tc.wantID, events[0].ID)
	}
}

// TestMemoryStreamManager_ReturnedSliceIsCopy ensures callers cannot mutate
// internal state through the returned slice.
func TestMemoryStreamManager_ReturnedSliceIsCopy(t *testing.T) {
	m := NewMemoryStreamManager()
	ctx := context.Background()

	require.NoError(t, m.AppendEvent(ctx, "s1", "m1", interfaces.StreamEvent{ID: "e1", Content: "orig"}))

	events, _, err := m.GetEvents(ctx, "s1", "m1", 0)
	require.NoError(t, err)
	events[0].Content = "mutated"

	again, _, err := m.GetEvents(ctx, "s1", "m1", 0)
	require.NoError(t, err)
	assert.Equal(t, "orig", again[0].Content)
}

func TestMemoryStreamManager_ConcurrentAppend(t *testing.T) {
	m := NewMemoryStreamManager()
	ctx := context.Background()
	const n = 100

	var wg sync.WaitGroup
	wg.Add(n)
	for i := 0; i < n; i++ {
		go func() {
			defer wg.Done()
			_ = m.AppendEvent(ctx, "s1", "m1", interfaces.StreamEvent{Type: types.ResponseTypeAnswer})
		}()
	}
	wg.Wait()

	events, next, err := m.GetEvents(ctx, "s1", "m1", 0)
	require.NoError(t, err)
	assert.Len(t, events, n)
	assert.Equal(t, n, next)
}

func TestMemoryStreamManager_ImplementsInterface(t *testing.T) {
	var _ interfaces.StreamManager = NewMemoryStreamManager()
}
