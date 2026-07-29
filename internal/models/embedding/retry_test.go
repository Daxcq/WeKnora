package embedding

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

type stubRoundTripper struct {
	requests []*http.Request
	respond  respondFunc
}

func (s *stubRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	s.requests = append(s.requests, req)
	return s.respond(len(s.requests), req)
}

type respondFunc func(attempt int, req *http.Request) (*http.Response, error)

func newStubClient(respond respondFunc) (*http.Client, *stubRoundTripper) {
	rt := &stubRoundTripper{respond: respond}
	return &http.Client{Transport: rt}, rt
}

func TestPostEmbeddingRequest_SetsHeadersAndBody(t *testing.T) {
	client, rt := newStubClient(func(_ int, req *http.Request) (*http.Response, error) {
		return httptest.NewRecorder().Result(), nil
	})

	resp, err := postEmbeddingRequest(context.Background(), client, embeddingPostRequest{
		provider:      "TestEmbedder",
		url:           "https://example.com/embeddings",
		body:          []byte(`{"input":"hi"}`),
		headers:       map[string]string{"Authorization": "Bearer secret"},
		customHeaders: map[string]string{"X-Trace": "abc"},
		maxRetries:    3,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer resp.Body.Close()

	if len(rt.requests) != 1 {
		t.Fatalf("expected a single attempt, got %d", len(rt.requests))
	}
	req := rt.requests[0]
	if req.Method != http.MethodPost {
		t.Fatalf("unexpected method: %s", req.Method)
	}
	if got := req.Header.Get("Content-Type"); got != "application/json" {
		t.Fatalf("unexpected Content-Type: %q", got)
	}
	if got := req.Header.Get("Authorization"); got != "Bearer secret" {
		t.Fatalf("unexpected Authorization: %q", got)
	}
	if got := req.Header.Get("X-Trace"); got != "abc" {
		t.Fatalf("custom header not applied: %q", got)
	}
}

func TestPostEmbeddingRequest_RetriesAndRebuildsBody(t *testing.T) {
	client, rt := newStubClient(func(attempt int, req *http.Request) (*http.Response, error) {
		body := make([]byte, 3)
		if _, err := req.Body.Read(body); err != nil {
			return nil, fmt.Errorf("attempt %d could not read body: %w", attempt, err)
		}
		if string(body) != "abc" {
			return nil, fmt.Errorf("attempt %d saw body %q", attempt, string(body))
		}
		if attempt < 2 {
			return nil, errors.New("connection refused")
		}
		return httptest.NewRecorder().Result(), nil
	})

	resp, err := postEmbeddingRequest(context.Background(), client, embeddingPostRequest{
		provider:   "TestEmbedder",
		url:        "https://example.com/embeddings",
		body:       []byte("abc"),
		maxRetries: 3,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer resp.Body.Close()

	if len(rt.requests) != 2 {
		t.Fatalf("expected 2 attempts, got %d", len(rt.requests))
	}
}

// Callers dereference resp.Body straight away, so an exhausted retry loop must
// never return (nil, nil).
func TestPostEmbeddingRequest_ExhaustedRetriesReturnError(t *testing.T) {
	client, rt := newStubClient(func(_ int, _ *http.Request) (*http.Response, error) {
		return nil, errors.New("connection refused")
	})

	resp, err := postEmbeddingRequest(context.Background(), client, embeddingPostRequest{
		provider:   "TestEmbedder",
		url:        "https://example.com/embeddings",
		body:       []byte("{}"),
		maxRetries: 1,
	})
	if resp != nil {
		t.Fatal("expected nil response")
	}
	if err == nil {
		t.Fatal("expected error after exhausting retries")
	}
	if len(rt.requests) != 2 {
		t.Fatalf("expected 2 attempts, got %d", len(rt.requests))
	}
}

func TestPostEmbeddingRequest_CanceledContextStopsRetries(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	client, rt := newStubClient(func(_ int, _ *http.Request) (*http.Response, error) {
		cancel()
		return nil, errors.New("connection refused")
	})

	_, err := postEmbeddingRequest(ctx, client, embeddingPostRequest{
		provider:   "TestEmbedder",
		url:        "https://example.com/embeddings",
		body:       []byte("{}"),
		maxRetries: 3,
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context cancellation, got %v", err)
	}
	if len(rt.requests) != 1 {
		t.Fatalf("expected retries to stop after cancellation, got %d attempts", len(rt.requests))
	}
}

func TestEmbedSingle(t *testing.T) {
	want := []float32{0.1, 0.2}
	got, err := embedSingle(context.Background(),
		func(_ context.Context, texts []string) ([][]float32, error) {
			if len(texts) != 1 || texts[0] != "hello" {
				t.Fatalf("unexpected batch input: %v", texts)
			}
			return [][]float32{want}, nil
		}, "hello")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != len(want) || got[0] != want[0] {
		t.Fatalf("unexpected embedding: %v", got)
	}
}

func TestEmbedSingle_PropagatesErrorAndEmptyResult(t *testing.T) {
	sentinel := errors.New("boom")
	if _, err := embedSingle(context.Background(),
		func(context.Context, []string) ([][]float32, error) { return nil, sentinel },
		"hello"); !errors.Is(err, sentinel) {
		t.Fatalf("expected sentinel error, got %v", err)
	}

	calls := 0
	_, err := embedSingle(context.Background(),
		func(context.Context, []string) ([][]float32, error) {
			calls++
			return nil, nil
		}, "hello")
	if err == nil {
		t.Fatal("expected error when no embedding is returned")
	}
	if calls != embedSingleRetries {
		t.Fatalf("expected %d attempts, got %d", embedSingleRetries, calls)
	}
}
