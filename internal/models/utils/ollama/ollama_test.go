package ollama

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/ollama/ollama/api"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// newTestService spins up an httptest server with the provided handler and
// returns an OllamaService pointed at it via GetOllamaService (OLLAMA_BASE_URL).
func newTestService(t *testing.T, handler http.Handler) *OllamaService {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)

	t.Setenv("OLLAMA_BASE_URL", srv.URL)
	t.Setenv("OLLAMA_OPTIONAL", "")

	svc, err := GetOllamaService()
	require.NoError(t, err)
	return svc
}

// tagsHandler builds a mux whose /api/tags returns the given model names.
func tagsMux(models ...string) *http.ServeMux {
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	mux.HandleFunc("/api/tags", func(w http.ResponseWriter, r *http.Request) {
		list := api.ListResponse{}
		for _, m := range models {
			list.Models = append(list.Models, api.ListModelResponse{
				Name:       m,
				Model:      m,
				Size:       123,
				Digest:     "sha256:abc",
				ModifiedAt: time.Unix(1700000000, 0),
			})
		}
		_ = json.NewEncoder(w).Encode(list)
	})
	return mux
}

func TestIsValidModelName(t *testing.T) {
	tests := []struct {
		name string
		want bool
	}{
		{"llama3", true},
		{"llama3:latest", true},
		{"", false},
		{"has space", false},
		{" leadingspace", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, IsValidModelName(tt.name))
		})
	}
}

func TestGetOllamaService_DefaultURL(t *testing.T) {
	t.Setenv("OLLAMA_BASE_URL", "")
	t.Setenv("OLLAMA_OPTIONAL", "")

	svc, err := GetOllamaService()
	require.NoError(t, err)
	assert.Equal(t, "http://localhost:11434", svc.baseURL)
	assert.False(t, svc.isOptional)
	assert.NotNil(t, svc.GetClient())
}

func TestGetOllamaService_CustomURLAndOptional(t *testing.T) {
	t.Setenv("OLLAMA_BASE_URL", "http://example.com:9999")
	t.Setenv("OLLAMA_OPTIONAL", "true")

	svc, err := GetOllamaService()
	require.NoError(t, err)
	assert.Equal(t, "http://example.com:9999", svc.baseURL)
	assert.True(t, svc.isOptional)
}

func TestGetOllamaService_InvalidURL(t *testing.T) {
	t.Setenv("OLLAMA_BASE_URL", "://missing-scheme")

	svc, err := GetOllamaService()
	require.Error(t, err)
	assert.Nil(t, svc)
}

func TestStartService_Available(t *testing.T) {
	svc := newTestService(t, tagsMux())

	require.NoError(t, svc.StartService(context.Background()))
	assert.True(t, svc.IsAvailable())
}

func TestStartService_UnavailableRequired(t *testing.T) {
	t.Setenv("OLLAMA_BASE_URL", "http://127.0.0.1:1")
	t.Setenv("OLLAMA_OPTIONAL", "")
	svc, err := GetOllamaService()
	require.NoError(t, err)

	err = svc.StartService(context.Background())
	require.Error(t, err)
	assert.False(t, svc.IsAvailable())
}

func TestStartService_UnavailableOptional(t *testing.T) {
	t.Setenv("OLLAMA_BASE_URL", "http://127.0.0.1:1")
	t.Setenv("OLLAMA_OPTIONAL", "true")
	svc, err := GetOllamaService()
	require.NoError(t, err)

	// Optional mode swallows the unavailable error.
	require.NoError(t, svc.StartService(context.Background()))
	assert.False(t, svc.IsAvailable())
}

func TestIsModelAvailable(t *testing.T) {
	svc := newTestService(t, tagsMux("llama3:latest", "qwen:7b"))
	ctx := context.Background()

	// ":latest" is appended when no tag is supplied.
	ok, err := svc.IsModelAvailable(ctx, "llama3")
	require.NoError(t, err)
	assert.True(t, ok)

	// Exact tagged match.
	ok, err = svc.IsModelAvailable(ctx, "qwen:7b")
	require.NoError(t, err)
	assert.True(t, ok)

	// Missing model.
	ok, err = svc.IsModelAvailable(ctx, "missing")
	require.NoError(t, err)
	assert.False(t, ok)
}

func TestIsModelAvailable_UnavailableOptional(t *testing.T) {
	t.Setenv("OLLAMA_BASE_URL", "http://127.0.0.1:1")
	t.Setenv("OLLAMA_OPTIONAL", "true")
	svc, err := GetOllamaService()
	require.NoError(t, err)

	ok, err := svc.IsModelAvailable(context.Background(), "llama3")
	require.NoError(t, err)
	assert.False(t, ok)
}

func TestListModels(t *testing.T) {
	svc := newTestService(t, tagsMux("a:latest", "b:latest"))

	names, err := svc.ListModels(context.Background())
	require.NoError(t, err)
	assert.Equal(t, []string{"a:latest", "b:latest"}, names)
}

func TestListModelsDetailed(t *testing.T) {
	svc := newTestService(t, tagsMux("a:latest"))

	models, err := svc.ListModelsDetailed(context.Background())
	require.NoError(t, err)
	require.Len(t, models, 1)
	assert.Equal(t, "a:latest", models[0].Name)
	assert.Equal(t, int64(123), models[0].Size)
	assert.Equal(t, "sha256:abc", models[0].Digest)
}

func TestListModels_Error(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/tags", func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	})
	svc := newTestService(t, mux)

	_, err := svc.ListModels(context.Background())
	require.Error(t, err)
	_, err = svc.ListModelsDetailed(context.Background())
	require.Error(t, err)
}

func TestGetVersion(t *testing.T) {
	mux := tagsMux()
	mux.HandleFunc("/api/version", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]string{"version": "0.1.2"})
	})
	svc := newTestService(t, mux)

	v, err := svc.GetVersion(context.Background())
	require.NoError(t, err)
	assert.Equal(t, "0.1.2", v)
}

func TestGetVersion_UnavailableOptional(t *testing.T) {
	t.Setenv("OLLAMA_BASE_URL", "http://127.0.0.1:1")
	t.Setenv("OLLAMA_OPTIONAL", "true")
	svc, err := GetOllamaService()
	require.NoError(t, err)
	// isAvailable defaults to false; optional short-circuits to a sentinel.

	v, err := svc.GetVersion(context.Background())
	require.NoError(t, err)
	assert.Equal(t, "unavailable", v)
}

func TestCreateModel(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/create", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(api.ProgressResponse{Status: "success"})
	})
	svc := newTestService(t, mux)

	require.NoError(t, svc.CreateModel(context.Background(), "custom", "FROM base"))
}

func TestGetModelInfo(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/show", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(api.ShowResponse{License: "MIT"})
	})
	svc := newTestService(t, mux)

	info, err := svc.GetModelInfo(context.Background(), "llama3")
	require.NoError(t, err)
	assert.Equal(t, "MIT", info.License)
}

func TestGetModelInfo_Error(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/show", func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "nope", http.StatusNotFound)
	})
	svc := newTestService(t, mux)

	_, err := svc.GetModelInfo(context.Background(), "llama3")
	require.Error(t, err)
}

func TestDeleteModel(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/delete", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	svc := newTestService(t, mux)

	require.NoError(t, svc.DeleteModel(context.Background(), "llama3"))
}

func TestPullModel_AlreadyExists(t *testing.T) {
	// Model already present: PullModel returns without hitting /api/pull.
	svc := newTestService(t, tagsMux("llama3:latest"))

	require.NoError(t, svc.PullModel(context.Background(), "llama3"))
}

func TestPullModel_Pulls(t *testing.T) {
	pulled := false
	mux := tagsMux() // empty list -> model considered missing
	mux.HandleFunc("/api/pull", func(w http.ResponseWriter, r *http.Request) {
		pulled = true
		_ = json.NewEncoder(w).Encode(api.ProgressResponse{
			Status: "downloading", Total: 100, Completed: 100,
		})
	})
	svc := newTestService(t, mux)

	require.NoError(t, svc.PullModel(context.Background(), "newmodel"))
	assert.True(t, pulled)
}

func TestEnsureModelAvailable_Present(t *testing.T) {
	svc := newTestService(t, tagsMux("llama3:latest"))
	require.NoError(t, svc.EnsureModelAvailable(context.Background(), "llama3"))
}

func TestEnsureModelAvailable_PullsWhenMissing(t *testing.T) {
	pulled := false
	mux := tagsMux()
	mux.HandleFunc("/api/pull", func(w http.ResponseWriter, r *http.Request) {
		pulled = true
		_ = json.NewEncoder(w).Encode(api.ProgressResponse{Status: "success"})
	})
	svc := newTestService(t, mux)

	require.NoError(t, svc.EnsureModelAvailable(context.Background(), "newmodel"))
	assert.True(t, pulled)
}

func TestEnsureModelAvailable_UnavailableOptional(t *testing.T) {
	t.Setenv("OLLAMA_BASE_URL", "http://127.0.0.1:1")
	t.Setenv("OLLAMA_OPTIONAL", "true")
	svc, err := GetOllamaService()
	require.NoError(t, err)

	// isAvailable is false and optional, so it no-ops without error.
	require.NoError(t, svc.EnsureModelAvailable(context.Background(), "llama3"))
}

func TestEmbeddings(t *testing.T) {
	mux := tagsMux()
	mux.HandleFunc("/api/embed", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(api.EmbedResponse{
			Embeddings: [][]float32{{0.1, 0.2}},
		})
	})
	svc := newTestService(t, mux)

	resp, err := svc.Embeddings(context.Background(), &api.EmbedRequest{Model: "m", Input: "hi"})
	require.NoError(t, err)
	require.Len(t, resp.Embeddings, 1)
	assert.Equal(t, []float32{0.1, 0.2}, resp.Embeddings[0])
}

func TestChat(t *testing.T) {
	mux := tagsMux()
	mux.HandleFunc("/api/chat", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(api.ChatResponse{
			Message: api.Message{Role: "assistant", Content: "hello"},
			Done:    true,
		})
	})
	svc := newTestService(t, mux)

	var got string
	err := svc.Chat(context.Background(),
		&api.ChatRequest{Model: "m", Messages: []api.Message{{Role: "user", Content: "hi"}}},
		func(resp api.ChatResponse) error {
			got += resp.Message.Content
			return nil
		},
	)
	require.NoError(t, err)
	assert.Equal(t, "hello", got)
}

func TestChat_ServiceDown(t *testing.T) {
	t.Setenv("OLLAMA_BASE_URL", "http://127.0.0.1:1")
	t.Setenv("OLLAMA_OPTIONAL", "")
	svc, err := GetOllamaService()
	require.NoError(t, err)

	err = svc.Chat(context.Background(), &api.ChatRequest{Model: "m"}, func(api.ChatResponse) error { return nil })
	require.Error(t, err)
}

func TestGenerate(t *testing.T) {
	mux := tagsMux()
	mux.HandleFunc("/api/generate", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(api.GenerateResponse{Response: "world", Done: true})
	})
	svc := newTestService(t, mux)

	var got string
	err := svc.Generate(context.Background(),
		&api.GenerateRequest{Model: "m", Prompt: "hi"},
		func(resp api.GenerateResponse) error {
			got += resp.Response
			return nil
		},
	)
	require.NoError(t, err)
	assert.Equal(t, "world", got)
}

func TestGenerate_ServiceDown(t *testing.T) {
	t.Setenv("OLLAMA_BASE_URL", "http://127.0.0.1:1")
	t.Setenv("OLLAMA_OPTIONAL", "")
	svc, err := GetOllamaService()
	require.NoError(t, err)

	err = svc.Generate(context.Background(), &api.GenerateRequest{Model: "m"}, func(api.GenerateResponse) error { return nil })
	require.Error(t, err)
}

func TestEmbeddings_ServiceDown(t *testing.T) {
	t.Setenv("OLLAMA_BASE_URL", "http://127.0.0.1:1")
	t.Setenv("OLLAMA_OPTIONAL", "")
	svc, err := GetOllamaService()
	require.NoError(t, err)

	_, err = svc.Embeddings(context.Background(), &api.EmbedRequest{Model: "m", Input: "hi"})
	require.Error(t, err)
}
