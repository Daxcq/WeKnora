package main

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestIncrementalOnlyReturnsChangedFiles(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "a.md"), []byte("a"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "c.md"), []byte("c"), 0600); err != nil {
		t.Fatal(err)
	}
	cfg, _ := json.Marshal(config{Settings: map[string]interface{}{"root_path": root}})
	connector := localConnector{}
	_, cursor, err := connector.FetchIncremental(context.Background(), cfg, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "c.md"), []byte("c changed"), 0600); err != nil {
		t.Fatal(err)
	}
	items, _, err := connector.FetchIncremental(context.Background(), cfg, cursor)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 || items[0].ExternalID != "c.md" || string(items[0].Content) != "c changed" {
		t.Fatalf("expected only c.md, got %#v", items)
	}
}

func TestIncrementalReportsDeletion(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "a.md"), []byte("a"), 0600); err != nil {
		t.Fatal(err)
	}
	cfg, _ := json.Marshal(config{Settings: map[string]interface{}{"root_path": root}})
	connector := localConnector{}
	_, cursor, err := connector.FetchIncremental(context.Background(), cfg, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(root, "a.md")); err != nil {
		t.Fatal(err)
	}
	items, _, err := connector.FetchIncremental(context.Background(), cfg, cursor)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 || items[0].ExternalID != "a.md" || !items[0].IsDeleted {
		t.Fatalf("expected deletion for a.md, got %#v", items)
	}
}

func TestIncrementalKeepsSelectedResourceScope(t *testing.T) {
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, "selected"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "selected", "a.md"), []byte("a"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "outside.md"), []byte("outside"), 0600); err != nil {
		t.Fatal(err)
	}
	cfg, _ := json.Marshal(config{Settings: map[string]interface{}{"root_path": root}, ResourceIDs: []string{"selected"}})
	items, _, err := (localConnector{}).FetchIncremental(context.Background(), cfg, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 || items[0].ExternalID != "selected/a.md" {
		t.Fatalf("expected only selected/a.md, got %#v", items)
	}
}
