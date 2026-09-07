package main

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"path"
	"sort"
	"strings"
	"time"

	"github.com/Tencent/WeKnora/pkg/pluginapi"
)

type githubConnector struct {
	client  *http.Client
	token   string
	baseURL string
}

type config struct {
	Credentials map[string]interface{} `json:"credentials"`
	Settings    map[string]interface{} `json:"settings"`
	ResourceIDs []string               `json:"resource_ids"`
}

type treeEntry struct {
	Path string `json:"path"`
	Mode string `json:"mode"`
	Type string `json:"type"`
	SHA  string `json:"sha"`
	Size int64  `json:"size"`
}

type treeResponse struct {
	Tree      []treeEntry `json:"tree"`
	Truncated bool        `json:"truncated"`
}

type refResponse struct {
	Object struct {
		SHA string `json:"sha"`
	} `json:"object"`
}

type blobResponse struct {
	Content  string `json:"content"`
	Encoding string `json:"encoding"`
}

type fileState struct {
	SHA  string `json:"sha"`
	Size int64  `json:"size"`
}

type cursor struct {
	ConnectorCursor map[string]interface{} `json:"connector_cursor"`
}

func (c githubConnector) settings(raw json.RawMessage) (config, string, string, string, string, error) {
	var cfg config
	if err := json.Unmarshal(raw, &cfg); err != nil {
		return cfg, "", "", "", "", err
	}
	owner := stringSetting(cfg.Settings, "owner")
	repository := stringSetting(cfg.Settings, "repository")
	branch := stringSetting(cfg.Settings, "branch")
	prefix := strings.Trim(stringSetting(cfg.Settings, "path"), "/")
	if owner == "" || repository == "" {
		return cfg, "", "", "", "", errors.New("settings.owner and settings.repository are required")
	}
	if branch == "" {
		branch = "main"
	}
	return cfg, owner, repository, branch, prefix, nil
}

func stringSetting(values map[string]interface{}, key string) string {
	value, _ := values[key].(string)
	return strings.TrimSpace(value)
}

func (c githubConnector) Validate(ctx context.Context, raw json.RawMessage) error {
	cfg, owner, repository, branch, _, err := c.settings(raw)
	if err != nil {
		return err
	}
	c = c.withToken(cfg)
	_, err = c.ref(ctx, owner, repository, branch)
	return err
}

func (c githubConnector) ListResources(ctx context.Context, raw json.RawMessage, parentID string) ([]pluginapi.Resource, error) {
	cfg, owner, repository, branch, prefix, err := c.settings(raw)
	if err != nil {
		return nil, err
	}
	c = c.withToken(cfg)
	tree, _, err := c.tree(ctx, owner, repository, branch)
	if err != nil {
		return nil, err
	}
	parentID = strings.Trim(parentID, "/")
	resources := make([]pluginapi.Resource, 0)
	seen := map[string]bool{}
	for _, entry := range tree {
		if entry.Type != "blob" && entry.Type != "tree" {
			continue
		}
		name := strings.TrimPrefix(entry.Path, prefix+"/")
		if prefix != "" && name == entry.Path {
			if entry.Path != prefix && !strings.HasPrefix(entry.Path, prefix+"/") {
				continue
			}
		}
		if parentID != "" && path.Dir(name) != parentID {
			continue
		}
		if parentID == "" && strings.Contains(name, "/") {
			continue
		}
		if seen[name] {
			continue
		}
		seen[name] = true
		resourceType := "file"
		hasChildren := false
		if entry.Type == "tree" {
			resourceType, hasChildren = "folder", true
		}
		resources = append(resources, pluginapi.Resource{ExternalID: name, Name: path.Base(name), Type: resourceType, ParentID: parentID, HasChildren: hasChildren, URL: c.webURL(owner, repository, branch, name)})
	}
	sort.Slice(resources, func(i, j int) bool { return resources[i].ExternalID < resources[j].ExternalID })
	return resources, nil
}

func (c githubConnector) ResolveResourceAncestors(_ context.Context, _ json.RawMessage, ids []string) ([]string, error) {
	seen := map[string]bool{}
	for _, id := range ids {
		id = strings.Trim(id, "/")
		for parent := path.Dir(id); parent != "." && parent != "/"; parent = path.Dir(parent) {
			seen[parent] = true
		}
	}
	ancestors := make([]string, 0, len(seen))
	for id := range seen {
		ancestors = append(ancestors, id)
	}
	sort.Strings(ancestors)
	return ancestors, nil
}

func (c githubConnector) FetchAll(ctx context.Context, raw json.RawMessage, resourceIDs []string) ([]pluginapi.FetchedItem, error) {
	cfg, owner, repository, branch, prefix, err := c.settings(raw)
	if err != nil {
		return nil, err
	}
	c = c.withToken(cfg)
	tree, _, err := c.tree(ctx, owner, repository, branch)
	if err != nil {
		return nil, err
	}
	return c.fetchEntries(ctx, owner, repository, branch, prefix, tree, resourceIDs)
}

func (c githubConnector) FetchIncremental(ctx context.Context, raw json.RawMessage, rawCursor json.RawMessage) ([]pluginapi.FetchedItem, json.RawMessage, error) {
	cfg, owner, repository, branch, prefix, err := c.settings(raw)
	if err != nil {
		return nil, nil, err
	}
	c = c.withToken(cfg)
	tree, commit, err := c.tree(ctx, owner, repository, branch)
	if err != nil {
		return nil, nil, err
	}
	old := readFiles(rawCursor)
	current := make(map[string]fileState)
	changed := make([]treeEntry, 0)
	for _, entry := range tree {
		if entry.Type != "blob" || !selected(entry.Path, prefix, cfg.ResourceIDs) {
			continue
		}
		current[entry.Path] = fileState{SHA: entry.SHA, Size: entry.Size}
		if previous, ok := old[entry.Path]; !ok || previous.SHA != entry.SHA {
			changed = append(changed, entry)
		}
	}
	for name := range old {
		if _, ok := current[name]; !ok {
			changed = append(changed, treeEntry{Path: name, Type: "deleted"})
		}
	}
	sort.Slice(changed, func(i, j int) bool { return changed[i].Path < changed[j].Path })
	items, err := c.fetchEntries(ctx, owner, repository, branch, prefix, changed, nil)
	if err != nil {
		return nil, nil, err
	}
	next, _ := json.Marshal(map[string]interface{}{"last_sync_time": time.Now().UTC(), "connector_cursor": map[string]interface{}{"commit": commit, "files": current}})
	return items, next, nil
}

func selected(name, prefix string, resourceIDs []string) bool {
	if prefix != "" && name != prefix && !strings.HasPrefix(name, prefix+"/") {
		return false
	}
	relative := strings.TrimPrefix(name, prefix+"/")
	if len(resourceIDs) == 0 {
		return true
	}
	for _, id := range resourceIDs {
		id = strings.Trim(id, "/")
		if name == id || relative == id || strings.HasPrefix(name, id+"/") || strings.HasPrefix(relative, id+"/") {
			return true
		}
	}
	return false
}

func (c githubConnector) fetchEntries(ctx context.Context, owner, repository, branch, prefix string, entries []treeEntry, resourceIDs []string) ([]pluginapi.FetchedItem, error) {
	items := make([]pluginapi.FetchedItem, 0)
	for _, entry := range entries {
		if entry.Type == "deleted" {
			items = append(items, pluginapi.FetchedItem{ExternalID: entry.Path, FileName: fetchedFileName(entry.Path), IsDeleted: true})
			continue
		}
		if entry.Type != "blob" || !selected(entry.Path, prefix, resourceIDs) {
			continue
		}
		content, err := c.blob(ctx, owner, repository, entry.SHA)
		if err != nil {
			return nil, err
		}
		items = append(items, pluginapi.FetchedItem{ExternalID: entry.Path, Title: strings.TrimSuffix(path.Base(entry.Path), path.Ext(entry.Path)), Content: content, ContentType: contentType(entry.Path), FileName: fetchedFileName(entry.Path), URL: c.webURL(owner, repository, branch, entry.Path), UpdatedAt: time.Now().UTC(), Metadata: map[string]string{"github_sha": entry.SHA}})
	}
	return items, nil
}

func fetchedFileName(name string) string {
	name = path.Base(name)
	if path.Ext(name) == "" {
		return name + ".txt"
	}
	return name
}

func contentType(name string) string {
	switch strings.ToLower(path.Ext(name)) {
	case ".md", ".markdown":
		return "text/markdown"
	case ".json":
		return "application/json"
	default:
		return "text/plain"
	}
}

func (c githubConnector) ref(ctx context.Context, owner, repository, branch string) (string, error) {
	var response refResponse
	err := c.get(ctx, "/repos/"+url.PathEscape(owner)+"/"+url.PathEscape(repository)+"/git/ref/heads/"+url.PathEscape(branch), &response)
	return response.Object.SHA, err
}

func (c githubConnector) tree(ctx context.Context, owner, repository, branch string) ([]treeEntry, string, error) {
	commit, err := c.ref(ctx, owner, repository, branch)
	if err != nil {
		return nil, "", err
	}
	var response treeResponse
	err = c.get(ctx, "/repos/"+url.PathEscape(owner)+"/"+url.PathEscape(repository)+"/git/trees/"+url.PathEscape(commit)+"?recursive=1", &response)
	if err == nil && response.Truncated {
		err = errors.New("github repository tree is truncated; narrow settings.path")
	}
	return response.Tree, commit, err
}

func (c githubConnector) blob(ctx context.Context, owner, repository, sha string) ([]byte, error) {
	var response blobResponse
	if err := c.get(ctx, "/repos/"+url.PathEscape(owner)+"/"+url.PathEscape(repository)+"/git/blobs/"+url.PathEscape(sha), &response); err != nil {
		return nil, err
	}
	if response.Encoding != "base64" {
		return nil, fmt.Errorf("unsupported GitHub blob encoding %q", response.Encoding)
	}
	return base64.StdEncoding.DecodeString(strings.Join(strings.Fields(response.Content), ""))
}

func (c githubConnector) get(ctx context.Context, endpoint string, out interface{}) error {
	base := c.baseURL
	if base == "" {
		base = "https://api.github.com"
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimRight(base, "/")+endpoint, nil)
	if err != nil {
		return err
	}
	request.Header.Set("Accept", "application/vnd.github+json")
	request.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	if c.token != "" {
		request.Header.Set("Authorization", "Bearer "+c.token)
	}
	response, err := c.client.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(response.Body, 4096))
		return fmt.Errorf("github API %s: %s", response.Status, strings.TrimSpace(string(body)))
	}
	return json.NewDecoder(response.Body).Decode(out)
}

func (c githubConnector) withToken(cfg config) githubConnector {
	c.token = stringSetting(cfg.Credentials, "token")
	return c
}

func (c githubConnector) webURL(owner, repository, branch, file string) string {
	return "https://github.com/" + url.PathEscape(owner) + "/" + url.PathEscape(repository) + "/blob/" + url.PathEscape(branch) + "/" + strings.ReplaceAll(file, " ", "%20")
}

func readFiles(raw []byte) map[string]fileState {
	var value cursor
	if json.Unmarshal(raw, &value) != nil {
		return nil
	}
	encoded, _ := json.Marshal(value.ConnectorCursor["files"])
	files := map[string]fileState{}
	_ = json.Unmarshal(encoded, &files)
	return files
}

func main() {
	manifest := pluginapi.Manifest{ID: "github", Name: "GitHub", Version: "0.1.0", ProtocolVersion: pluginapi.ProtocolVersion, ExtensionTypes: []string{"datasource"}, WeKnoraVersion: ">=0.6.0 <1.0.0", Permissions: pluginapi.Permissions{AllowNetwork: true}, Runtime: pluginapi.Runtime{Type: "docker", Image: "weknora-plugin-github:dev"}, Description: "Sync files from a GitHub repository."}
	if err := pluginapi.Serve(manifest, githubConnector{client: &http.Client{Timeout: 30 * time.Second}}); err != nil {
		log.Fatal(err)
	}
}
