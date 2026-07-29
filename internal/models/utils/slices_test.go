package utils

import (
	"strconv"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestChunkSlice(t *testing.T) {
	tests := []struct {
		name      string
		input     []int
		chunkSize int
		want      [][]int
	}{
		{"empty slice", []int{}, 3, [][]int{}},
		{"nil slice", nil, 3, [][]int{}},
		{"exact multiple", []int{1, 2, 3, 4}, 2, [][]int{{1, 2}, {3, 4}}},
		{"with remainder", []int{1, 2, 3, 4, 5}, 2, [][]int{{1, 2}, {3, 4}, {5}}},
		{"chunk larger than slice", []int{1, 2}, 5, [][]int{{1, 2}}},
		{"chunk size one", []int{1, 2, 3}, 1, [][]int{{1}, {2}, {3}}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, ChunkSlice(tt.input, tt.chunkSize))
		})
	}
}

func TestChunkSlice_String(t *testing.T) {
	got := ChunkSlice([]string{"a", "b", "c"}, 2)
	assert.Equal(t, [][]string{{"a", "b"}, {"c"}}, got)
}

func TestChunkSlice_PanicsOnNonPositiveSize(t *testing.T) {
	// A non-empty slice with a non-positive chunk size is a programming
	// error and must panic; an empty slice returns early before the check.
	assert.PanicsWithValue(t, "chunkSize must be greater than 0", func() {
		ChunkSlice([]int{1}, 0)
	})
	assert.PanicsWithValue(t, "chunkSize must be greater than 0", func() {
		ChunkSlice([]int{1}, -1)
	})
	assert.NotPanics(t, func() {
		ChunkSlice([]int{}, 0)
	})
}

func TestMapSlice(t *testing.T) {
	got := MapSlice([]int{1, 2, 3}, func(n int) string {
		return strconv.Itoa(n * 2)
	})
	assert.Equal(t, []string{"2", "4", "6"}, got)
}

func TestMapSlice_Empty(t *testing.T) {
	got := MapSlice([]int{}, func(n int) int { return n })
	assert.Empty(t, got)
	assert.NotNil(t, got)
}
