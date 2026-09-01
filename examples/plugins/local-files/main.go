package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/Tencent/WeKnora/pkg/pluginapi"
)

type localConnector struct{}

type config struct {
	Settings    map[string]interface{} `json:"settings"`
	ResourceIDs []string               `json:"resource_ids"`
}

type fileState struct {
	Hash      string    `json:"hash"`
	UpdatedAt time.Time `json:"updated_at"`
}

type cursor struct {
	LastSyncTime    time.Time              `json:"last_sync_time"`
	ConnectorCursor map[string]interface{} `json:"connector_cursor"`
}

func (localConnector) root(raw json.RawMessage) (string, error) {
	var cfg config
	if err := json.Unmarshal(raw, &cfg); err != nil {
		return "", err
	}
	value, ok := cfg.Settings["root_path"].(string)
	if !ok || strings.TrimSpace(value) == "" {
		return "", fmt.Errorf("settings.root_path is required")
	}
	info, err := os.Stat(value)
	if err != nil {
		return "", err
	}
	if !info.IsDir() {
		return "", fmt.Errorf("root_path is not a directory")
	}
	return value, nil
}

func (c localConnector) Validate(_ context.Context, raw json.RawMessage) error {
	_, err := c.root(raw)
	return err
}

func (c localConnector) ListResources(_ context.Context, raw json.RawMessage, parentID string) ([]pluginapi.Resource, error) {
	root, err := c.root(raw)
	if err != nil {
		return nil, err
	}
	base := root
	if parentID != "" {
		base = filepath.Join(root, filepath.FromSlash(parentID))
	}
	entries, err := os.ReadDir(base)
	if err != nil {
		return nil, err
	}
	resources := make([]pluginapi.Resource, 0, len(entries))
	for _, entry := range entries {
		rel, _ := filepath.Rel(root, filepath.Join(base, entry.Name()))
		resources = append(resources, pluginapi.Resource{ExternalID: filepath.ToSlash(rel), Name: entry.Name(), Type: resourceType(entry), ParentID: parentID, HasChildren: entry.IsDir()})
	}
	return resources, nil
}

func (c localConnector) ResolveResourceAncestors(_ context.Context, raw json.RawMessage, ids []string) ([]string, error) {
	root, err := c.root(raw)
	if err != nil {
		return nil, err
	}
	seen := map[string]bool{}
	for _, id := range ids {
		parts := strings.Split(filepath.ToSlash(id), "/")
		for i := 1; i < len(parts); i++ {
			parent := strings.Join(parts[:i], "/")
			if _, statErr := os.Stat(filepath.Join(root, filepath.FromSlash(parent))); statErr == nil {
				seen[parent] = true
			}
		}
	}
	out := make([]string, 0, len(seen))
	for id := range seen {
		out = append(out, id)
	}
	sort.Strings(out)
	return out, nil
}

func (c localConnector) FetchAll(_ context.Context, raw json.RawMessage, resourceIDs []string) ([]pluginapi.FetchedItem, error) {
	root, err := c.root(raw)
	if err != nil {
		return nil, err
	}
	files, err := scanFiles(root, resourceIDs)
	if err != nil {
		return nil, err
	}
	return readItems(root, files)
}

func (c localConnector) FetchIncremental(_ context.Context, raw json.RawMessage, rawCursor json.RawMessage) ([]pluginapi.FetchedItem, json.RawMessage, error) {
	root, err := c.root(raw)
	if err != nil {
		return nil, nil, err
	}
	var cfg config
	if err := json.Unmarshal(raw, &cfg); err != nil {
		return nil, nil, err
	}
	files, err := scanFiles(root, cfg.ResourceIDs)
	if err != nil {
		return nil, nil, err
	}
	old := readCursor(rawCursor)
	current := make(map[string]fileState, len(files))
	var changed []string
	for id, state := range files {
		current[id] = state
		if previous, ok := old[id]; !ok || previous.Hash != state.Hash {
			changed = append(changed, id)
		}
	}
	for id := range old {
		if _, ok := current[id]; !ok {
			changed = append(changed, id)
		}
	}
	sort.Strings(changed)
	items := make([]pluginapi.FetchedItem, 0, len(changed))
	for _, id := range changed {
		state, exists := current[id]
		if !exists {
			items = append(items, pluginapi.FetchedItem{ExternalID: id, FileName: filepath.Base(id), IsDeleted: true})
			continue
		}
		item, err := readItem(root, id, state)
		if err != nil {
			return nil, nil, err
		}
		items = append(items, item)
	}
	next, _ := json.Marshal(cursor{LastSyncTime: time.Now().UTC(), ConnectorCursor: map[string]interface{}{"files": current}})
	return items, next, nil
}

func scanFiles(root string, resourceIDs []string) (map[string]fileState, error) {
	selected := map[string]bool{}
	for _, id := range resourceIDs {
		selected[filepath.ToSlash(filepath.Clean(id))] = true
	}
	files := map[string]fileState{}
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			return nil
		}
		rel, _ := filepath.Rel(root, path)
		id := filepath.ToSlash(rel)
		if len(selected) > 0 && !selected[id] && !hasSelectedParent(id, selected) {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		hash := sha256.Sum256(data)
		files[id] = fileState{Hash: hex.EncodeToString(hash[:]), UpdatedAt: info.ModTime().UTC()}
		return nil
	})
	return files, err
}

func readItems(root string, files map[string]fileState) ([]pluginapi.FetchedItem, error) {
	ids := make([]string, 0, len(files))
	for id := range files {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	items := make([]pluginapi.FetchedItem, 0, len(ids))
	for _, id := range ids {
		item, err := readItem(root, id, files[id])
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, nil
}

func readItem(root, id string, state fileState) (pluginapi.FetchedItem, error) {
	data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(id)))
	if err != nil {
		return pluginapi.FetchedItem{}, err
	}
	return pluginapi.FetchedItem{ExternalID: id, Title: strings.TrimSuffix(filepath.Base(id), filepath.Ext(id)), Content: data, ContentType: "text/plain", FileName: filepath.Base(id), UpdatedAt: state.UpdatedAt, Metadata: map[string]string{"content_hash": state.Hash}}, nil
}

func readCursor(raw []byte) map[string]fileState {
	var c cursor
	if json.Unmarshal(raw, &c) != nil || c.ConnectorCursor == nil {
		return nil
	}
	data, _ := json.Marshal(c.ConnectorCursor["files"])
	out := map[string]fileState{}
	_ = json.Unmarshal(data, &out)
	return out
}
func hasSelectedParent(id string, selected map[string]bool) bool {
	for parent := range selected {
		if strings.HasPrefix(id, parent+"/") {
			return true
		}
	}
	return false
}
func resourceType(entry os.DirEntry) string {
	if entry.IsDir() {
		return "folder"
	}
	return "file"
}

func main() {
	manifest := pluginapi.Manifest{ID: "local-files", Name: "Local Files", Version: "0.1.0", ProtocolVersion: pluginapi.ProtocolVersion, ExtensionTypes: []string{"datasource"}, WeKnoraVersion: ">=0.6.0 <1.0.0", Permissions: pluginapi.Permissions{AllowNetwork: false}, Runtime: pluginapi.Runtime{Type: "docker", Image: "weknora-plugin-local-files:dev"}, Description: "Read a local directory with hash-based incremental sync."}
	if err := pluginapi.Serve(manifest, localConnector{}); err != nil {
		panic(err)
	}
}
