package errors

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestSessionSentinelErrors(t *testing.T) {
	tests := []struct {
		err  error
		want string
	}{
		{ErrSessionNotFound, "session not found"},
		{ErrSessionExpired, "session expired"},
		{ErrSessionLimitExceeded, "session limit exceeded"},
		{ErrInvalidSessionID, "invalid session id"},
		{ErrInvalidTenantID, "invalid tenant id"},
	}
	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			assert.EqualError(t, tt.err, tt.want)
		})
	}

	// Sentinels are distinct values, so comparisons don't collide.
	assert.NotErrorIs(t, ErrSessionNotFound, ErrSessionExpired)
}

func TestParseErrorCodeConstants(t *testing.T) {
	// These string codes are a stable wire contract with the frontend;
	// pin their values so an accidental rename is caught in review.
	assert.Equal(t, "DOCREADER_TIMEOUT", ErrCodeDocReaderTimeout)
	assert.Equal(t, "DOCREADER_UNAVAILABLE", ErrCodeDocReaderUnavailable)
	assert.Equal(t, "DOCREADER_PARSE_FAILED", ErrCodeDocReaderParseFailed)
	assert.Equal(t, "CHUNKING_FAILED", ErrCodeChunkingFailed)
	assert.Equal(t, "EMBEDDING_RATE_LIMIT", ErrCodeEmbeddingRateLimit)
	assert.Equal(t, "EMBEDDING_PROVIDER_FAIL", ErrCodeEmbeddingProviderFail)
	assert.Equal(t, "VECTORSTORE_WRITE_FAILED", ErrCodeVectorStoreWriteFailed)
	assert.Equal(t, "MULTIMODAL_VLM_FAILED", ErrCodeMultimodalVLMFailed)
	assert.Equal(t, "MULTIMODAL_ALL_FAILED", ErrCodeMultimodalAllFailed)
	assert.Equal(t, "TASK_TIMEOUT", ErrCodeTaskTimeout)
	assert.Equal(t, "UNKNOWN", ErrCodeUnknown)
}
