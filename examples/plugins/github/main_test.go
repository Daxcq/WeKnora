package main

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"sync"
	"testing"
)

func TestFetchIncrementalReturnsOnlyChangedFile(t *testing.T) {
	var mu sync.RWMutex
	commit := "commit-1"
	trees := map[string][]treeEntry{
		"commit-1": {{Path: "a.md", Type: "blob", SHA: "sha-a", Size: 1}, {Path: "b.md", Type: "blob", SHA: "sha-b", Size: 1}},
		"commit-2": {{Path: "a.md", Type: "blob", SHA: "sha-a", Size: 1}, {Path: "b.md", Type: "blob", SHA: "sha-b2", Size: 2}},
	}
	contents := map[string]string{
		"sha-a":  "A",
		"sha-b":  "B",
		"sha-b2": "B2",
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.RLock()
		defer mu.RUnlock()
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/repos/acme/docs/git/ref/heads/main":
			_ = json.NewEncoder(w).Encode(map[string]interface{}{"object": map[string]string{"sha": commit}})
		case "/repos/acme/docs/git/trees/commit-1":
			_ = json.NewEncoder(w).Encode(map[string]interface{}{"tree": trees["commit-1"]})
		case "/repos/acme/docs/git/trees/commit-2":
			_ = json.NewEncoder(w).Encode(map[string]interface{}{"tree": trees["commit-2"]})
		default:
			const prefix = "/repos/acme/docs/git/blobs/"
			if len(r.URL.Path) > len(prefix) && r.URL.Path[:len(prefix)] == prefix {
				sha := r.URL.Path[len(prefix):]
				_ = json.NewEncoder(w).Encode(map[string]string{"encoding": "base64", "content": base64.StdEncoding.EncodeToString([]byte(contents[sha]))})
				return
			}
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	raw, _ := json.Marshal(map[string]interface{}{
		"settings": map[string]string{"owner": "acme", "repository": "docs", "branch": "main"},
	})
	connector := githubConnector{client: server.Client(), baseURL: server.URL}
	first, cursor, err := connector.FetchIncremental(context.Background(), raw, nil)
	if err != nil {
		t.Fatalf("first sync: %v", err)
	}
	if len(first) != 2 {
		t.Fatalf("first sync returned %d items, want 2", len(first))
	}

	mu.Lock()
	commit = "commit-2"
	mu.Unlock()
	second, _, err := connector.FetchIncremental(context.Background(), raw, cursor)
	if err != nil {
		t.Fatalf("incremental sync: %v", err)
	}
	if len(second) != 1 || second[0].ExternalID != "b.md" || string(second[0].Content) != "B2" {
		t.Fatalf("incremental sync returned %#v, want only changed b.md", second)
	}
}

func TestPublicRepository(t *testing.T) {
	if os.Getenv("GITHUB_INTEGRATION") != "1" {
		t.Skip("set GITHUB_INTEGRATION=1 to call the public GitHub API")
	}
	raw, _ := json.Marshal(map[string]interface{}{
		"settings": map[string]string{"owner": "octocat", "repository": "Hello-World", "branch": "master"},
	})
	connector := githubConnector{client: http.DefaultClient}
	first, cursor, err := connector.FetchIncremental(context.Background(), raw, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(first) == 0 {
		t.Fatal("first sync returned no files")
	}
	second, _, err := connector.FetchIncremental(context.Background(), raw, cursor)
	if err != nil {
		t.Fatal(err)
	}
	if len(second) != 0 {
		t.Fatalf("unchanged repository returned %d files", len(second))
	}
}
