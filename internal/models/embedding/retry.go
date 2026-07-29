package embedding

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/Tencent/WeKnora/internal/logger"
	secutils "github.com/Tencent/WeKnora/internal/utils"
)

// maxEmbeddingRetryBackoff caps the exponential backoff between attempts.
const maxEmbeddingRetryBackoff = 10 * time.Second

// embedSingleRetries is how many times embedSingle re-runs a batch call that
// succeeds but yields no vector.
const embedSingleRetries = 3

// embeddingPostRequest describes one retryable POST to an embedding provider.
type embeddingPostRequest struct {
	// provider labels log lines, e.g. "OpenAIEmbedder".
	provider string
	url      string
	body     []byte
	// headers carries provider auth headers; Content-Type is set automatically.
	headers map[string]string
	// customHeaders are user-supplied extra headers; reserved names are skipped.
	customHeaders map[string]string
	maxRetries    int
}

// postEmbeddingRequest POSTs r.body to r.url, retrying transport failures with
// exponential backoff capped at maxEmbeddingRetryBackoff. The request is rebuilt
// on every attempt so its body reader stays valid.
//
// A nil response is always paired with a non-nil error: callers dereference
// resp.Body straight away, so returning (nil, nil) would panic them.
func postEmbeddingRequest(
	ctx context.Context,
	client *http.Client,
	r embeddingPostRequest,
) (*http.Response, error) {
	var lastErr error

	for attempt := 0; attempt <= r.maxRetries; attempt++ {
		if attempt > 0 {
			backoff := time.Duration(1<<uint(attempt-1)) * time.Second
			if backoff > maxEmbeddingRetryBackoff {
				backoff = maxEmbeddingRetryBackoff
			}
			logger.GetLogger(ctx).
				Infof("%s retrying request (%d/%d), waiting %v", r.provider, attempt, r.maxRetries, backoff)

			select {
			case <-time.After(backoff):
			case <-ctx.Done():
				return nil, ctx.Err()
			}
		}

		req, err := http.NewRequestWithContext(ctx, http.MethodPost, r.url, bytes.NewReader(r.body))
		if err != nil {
			lastErr = err
			logger.GetLogger(ctx).Errorf("%s failed to create request: %v", r.provider, err)
			continue
		}
		req.Header.Set("Content-Type", "application/json")
		for name, value := range r.headers {
			req.Header.Set(name, value)
		}
		secutils.ApplyCustomHeaders(req, r.customHeaders)

		resp, err := client.Do(req)
		if err == nil {
			return resp, nil
		}
		lastErr = err
		logger.GetLogger(ctx).
			Errorf("%s request failed (attempt %d/%d): %v", r.provider, attempt+1, r.maxRetries+1, err)
	}

	if lastErr == nil {
		lastErr = fmt.Errorf("%s request failed after %d attempts", r.provider, r.maxRetries+1)
	}
	return nil, lastErr
}

// embedSingle adapts a batch embedder to the single-text Embed contract.
func embedSingle(
	ctx context.Context,
	batchEmbed func(context.Context, []string) ([][]float32, error),
	text string,
) ([]float32, error) {
	for range embedSingleRetries {
		embeddings, err := batchEmbed(ctx, []string{text})
		if err != nil {
			return nil, err
		}
		if len(embeddings) > 0 {
			return embeddings[0], nil
		}
	}
	return nil, fmt.Errorf("no embedding returned")
}
