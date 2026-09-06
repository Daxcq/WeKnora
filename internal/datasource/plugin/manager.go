package plugin

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/Tencent/WeKnora/internal/datasource"
	"github.com/Tencent/WeKnora/internal/infrastructure/docparser"
	infra_web_search "github.com/Tencent/WeKnora/internal/infrastructure/web_search"
	"github.com/Tencent/WeKnora/internal/logger"
	"github.com/Tencent/WeKnora/internal/models/chat"
	"github.com/Tencent/WeKnora/internal/models/provider"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"github.com/Tencent/WeKnora/pkg/pluginapi"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"gopkg.in/yaml.v3"
)

// Manager loads external connector plugins and owns their process lifetimes.
type Manager struct {
	mu         sync.Mutex
	connectors []*ProcessConnector
	parsers    []string
	models     []provider.ProviderName
}

func NewManager() *Manager { return &Manager{} }

func (m *Manager) LoadDirectory(ctx context.Context, registry *datasource.ConnectorRegistry, searchRegistry *infra_web_search.Registry, dir string) error {
	if strings.TrimSpace(dir) == "" {
		return nil
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return fmt.Errorf("read plugin directory: %w", err)
	}
	var errs []error
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		manifestPath, err := findManifest(filepath.Join(dir, entry.Name()))
		if err != nil {
			continue
		}
		connector, err := NewProcessPlugin(ctx, manifestPath)
		if err != nil {
			errs = append(errs, fmt.Errorf("load plugin %s: %w", entry.Name(), err))
			continue
		}
		hasDatasource := contains(connector.manifest.ExtensionTypes, "datasource")
		hasParser := contains(connector.manifest.ExtensionTypes, "document_parser")
		hasWebSearch := contains(connector.manifest.ExtensionTypes, "web_search")
		hasModel := contains(connector.manifest.ExtensionTypes, "model_provider")
		if !hasDatasource && !hasParser && !hasWebSearch && !hasModel {
			_ = connector.Close()
			errs = append(errs, fmt.Errorf("plugin %s provides no supported extension", entry.Name()))
			continue
		}
		if hasWebSearch {
			info := webSearchMetadata(connector.manifest)
			if err := searchRegistry.RegisterExternal(info, func(params types.WebSearchProviderParameters) (interfaces.WebSearchProvider, error) {
				return &processWebSearchProvider{connector: connector, params: params}, nil
			}); err != nil {
				_ = connector.Close()
				errs = append(errs, fmt.Errorf("register web search plugin %s: %w", entry.Name(), err))
				continue
			}
		}
		if hasModel {
			if len(connector.manifest.ModelTypes) != 1 || !strings.EqualFold(connector.manifest.ModelTypes[0], "chat") {
				_ = connector.Close()
				errs = append(errs, fmt.Errorf("model plugin %s currently supports only model_types [chat]", entry.Name()))
				continue
			}
			modelName := provider.ProviderName(connector.Type())
			info := modelProviderMetadata(connector.manifest)
			validate := func(config *provider.Config) error {
				checkCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
				defer cancel()
				response, err := connector.client.ValidateModelConfig(checkCtx, &pluginapi.ModelValidateRequest{Config: modelConfigFromProvider(config)})
				if err != nil {
					return err
				}
				if response.Error != "" {
					return errors.New(response.Error)
				}
				return nil
			}
			if err := provider.RegisterExternal(info, validate, connector.Health); err != nil {
				if hasWebSearch {
					searchRegistry.UnregisterExternal(connector.Type())
				}
				_ = connector.Close()
				errs = append(errs, fmt.Errorf("register model plugin %s: %w", entry.Name(), err))
				continue
			}
			if err := chat.RegisterExternalChat(modelName, func(cfg *chat.ChatConfig) (chat.Chat, error) {
				return &processChatModel{connector: connector, config: cfg}, nil
			}); err != nil {
				provider.UnregisterExternal(modelName)
				if hasWebSearch {
					searchRegistry.UnregisterExternal(connector.Type())
				}
				_ = connector.Close()
				errs = append(errs, fmt.Errorf("register model runtime %s: %w", entry.Name(), err))
				continue
			}
		}
		if hasParser {
			if err := docparser.RegisterExternalParser(connector.Type(), connector, types.ParserEngineInfo{
				Name: connector.Type(), Description: connector.manifest.Description,
				FileTypes: connector.manifest.FileTypes,
				Available: true, External: true,
			}); err != nil {
				if hasWebSearch {
					searchRegistry.UnregisterExternal(connector.Type())
				}
				if hasModel {
					provider.UnregisterExternal(provider.ProviderName(connector.Type()))
					chat.UnregisterExternalChat(provider.ProviderName(connector.Type()))
				}
				_ = connector.Close()
				errs = append(errs, fmt.Errorf("register parser plugin %s: %w", entry.Name(), err))
				continue
			}
		}
		if hasDatasource {
			if err := registry.Register(connector); err != nil {
				if hasParser {
					docparser.UnregisterExternalParser(connector.Type())
				}
				if hasWebSearch {
					searchRegistry.UnregisterExternal(connector.Type())
				}
				if hasModel {
					provider.UnregisterExternal(provider.ProviderName(connector.Type()))
					chat.UnregisterExternalChat(provider.ProviderName(connector.Type()))
				}
				_ = connector.Close()
				errs = append(errs, fmt.Errorf("register plugin %s: %w", entry.Name(), err))
				continue
			}
			datasource.ConnectorMetadataRegistry[connector.Type()] = datasource.ConnectorMetadata{
				Type: connector.Type(), Name: connector.manifest.Name, Description: connector.manifest.Description,
				AuthType: "custom", Capabilities: []string{"incremental"}, Config: configMetadata(connector.manifest.Config), External: true,
				Permissions: &datasource.ConnectorPermissions{AllowNetwork: connector.manifest.Permissions.AllowNetwork, ReadPaths: connector.manifest.Permissions.ReadPaths},
			}
		}
		m.mu.Lock()
		m.connectors = append(m.connectors, connector)
		if hasParser {
			m.parsers = append(m.parsers, connector.Type())
		}
		if hasModel {
			m.models = append(m.models, provider.ProviderName(connector.Type()))
		}
		m.mu.Unlock()
		if hasDatasource {
			logger.Infof(ctx, "external datasource plugin loaded: id=%s version=%s", connector.manifest.ID, connector.manifest.Version)
		}
		if hasParser {
			logger.Infof(ctx, "external document parser plugin loaded: id=%s version=%s", connector.manifest.ID, connector.manifest.Version)
		}
		if hasWebSearch {
			logger.Infof(ctx, "external web search plugin loaded: id=%s version=%s", connector.manifest.ID, connector.manifest.Version)
		}
		if hasModel {
			logger.Infof(ctx, "external model provider plugin loaded: id=%s version=%s", connector.manifest.ID, connector.manifest.Version)
		}
	}
	return errors.Join(errs...)
}

func webSearchMetadata(manifest pluginapi.Manifest) types.WebSearchProviderTypeInfo {
	info := types.WebSearchProviderTypeInfo{
		ID: manifest.ID, Name: manifest.Name, Description: manifest.Description,
		External: true,
		Permissions: &types.WebSearchProviderPermissions{
			AllowNetwork: manifest.Permissions.AllowNetwork, ReadPaths: manifest.Permissions.ReadPaths,
		},
	}
	for _, field := range manifest.Config {
		switch field.Name {
		case "api_key":
			info.RequiresAPIKey = field.Required
		case "engine_id":
			info.RequiresEngineID = field.Required
		case "base_url":
			info.RequiresBaseURL = field.Required
		case "proxy_url":
			info.SupportsProxy = true
		}
	}
	return info
}

func modelProviderMetadata(manifest pluginapi.Manifest) provider.ProviderInfo {
	info := provider.ProviderInfo{
		Name: provider.ProviderName(manifest.ID), DisplayName: manifest.Name,
		Description: manifest.Description, External: true,
		DefaultURLs: map[types.ModelType]string{},
		Permissions: &provider.ProviderPermissions{
			AllowNetwork: manifest.Permissions.AllowNetwork, ReadPaths: manifest.Permissions.ReadPaths,
		},
	}
	for _, modelType := range manifest.ModelTypes {
		switch strings.ToLower(modelType) {
		case "chat":
			info.ModelTypes = append(info.ModelTypes, types.ModelTypeKnowledgeQA)
		case "embedding":
			info.ModelTypes = append(info.ModelTypes, types.ModelTypeEmbedding)
		case "rerank":
			info.ModelTypes = append(info.ModelTypes, types.ModelTypeRerank)
		case "vllm":
			info.ModelTypes = append(info.ModelTypes, types.ModelTypeVLLM)
		case "asr":
			info.ModelTypes = append(info.ModelTypes, types.ModelTypeASR)
		}
	}
	for _, field := range manifest.Config {
		info.ExtraFields = append(info.ExtraFields, provider.ExtraFieldConfig{
			Key: field.Name, Label: field.Name, Type: field.Type,
			Required: field.Required,
		})
	}
	return info
}

func configMetadata(fields []pluginapi.ConfigField) []datasource.ConnectorConfigField {
	result := make([]datasource.ConnectorConfigField, len(fields))
	for i, field := range fields {
		result[i] = datasource.ConnectorConfigField{
			Name: field.Name, Type: field.Type, Required: field.Required,
			Secret: field.Secret, Description: field.Description,
		}
	}
	return result
}

func (m *Manager) Close() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	var errs []error
	for _, name := range m.parsers {
		docparser.UnregisterExternalParser(name)
	}
	m.parsers = nil
	for _, name := range m.models {
		chat.UnregisterExternalChat(name)
		provider.UnregisterExternal(name)
	}
	m.models = nil
	for _, connector := range m.connectors {
		if err := connector.Close(); err != nil {
			errs = append(errs, err)
		}
	}
	m.connectors = nil
	return errors.Join(errs...)
}

func findManifest(dir string) (string, error) {
	for _, name := range []string{"manifest.json", "manifest.yaml", "manifest.yml"} {
		path := filepath.Join(dir, name)
		if _, err := os.Stat(path); err == nil {
			return path, nil
		}
	}
	return "", os.ErrNotExist
}

func readManifest(path string) (pluginapi.Manifest, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return pluginapi.Manifest{}, err
	}
	var manifest pluginapi.Manifest
	if strings.HasSuffix(path, ".json") {
		err = json.Unmarshal(data, &manifest)
	} else {
		err = yaml.Unmarshal(data, &manifest)
	}
	if err != nil {
		return pluginapi.Manifest{}, fmt.Errorf("parse manifest: %w", err)
	}
	if err := pluginapi.ValidateManifest(manifest); err != nil {
		return pluginapi.Manifest{}, err
	}
	hostVersion := strings.TrimSpace(os.Getenv("WEKNORA_VERSION"))
	if hostVersion == "" {
		if data, readErr := os.ReadFile("VERSION"); readErr == nil {
			hostVersion = strings.TrimSpace(string(data))
		}
	}
	if hostVersion != "" && !pluginapi.VersionCompatible(hostVersion, manifest.WeKnoraVersion) {
		return pluginapi.Manifest{}, fmt.Errorf("plugin requires WeKnora %q, host is %q", manifest.WeKnoraVersion, hostVersion)
	}
	return manifest, nil
}

type ProcessConnector struct {
	manifest      pluginapi.Manifest
	cmd           *exec.Cmd
	conn          *grpc.ClientConn
	client        pluginapi.PluginClient
	containerName string
	mu            sync.Mutex
}

var _ datasource.Connector = (*ProcessConnector)(nil)

func NewProcessConnector(ctx context.Context, manifestPath string) (*ProcessConnector, error) {
	connector, err := NewProcessPlugin(ctx, manifestPath)
	if err != nil {
		return nil, err
	}
	if !contains(connector.manifest.ExtensionTypes, "datasource") {
		_ = connector.Close()
		return nil, fmt.Errorf("plugin %q does not provide datasource extension", connector.manifest.ID)
	}
	return connector, nil
}

func NewProcessPlugin(ctx context.Context, manifestPath string) (*ProcessConnector, error) {
	manifest, err := readManifest(manifestPath)
	if err != nil {
		return nil, err
	}
	if !contains(manifest.ExtensionTypes, "datasource") && !contains(manifest.ExtensionTypes, "document_parser") && !contains(manifest.ExtensionTypes, "web_search") && !contains(manifest.ExtensionTypes, "model_provider") {
		return nil, fmt.Errorf("plugin %q provides no supported extension", manifest.ID)
	}
	if manifest.Runtime.Type == "process" && !manifest.Permissions.AllowNetwork {
		return nil, fmt.Errorf("plugin %q requests network isolation but process runtime cannot enforce it; use docker runtime", manifest.ID)
	}
	if manifest.Runtime.Type != "process" && manifest.Runtime.Type != "docker" {
		return nil, fmt.Errorf("unsupported plugin runtime %q", manifest.Runtime.Type)
	}

	pluginDir := filepath.Dir(manifestPath)
	command := manifest.Runtime.Command
	if !filepath.IsAbs(command) {
		command = filepath.Join(pluginDir, command)
	}
	var cmd *exec.Cmd
	var address string
	var containerName string
	var transport net.Conn
	if manifest.Runtime.Type == "docker" {
		cmd, containerName, err = newDockerCommand(manifest, pluginDir)
	} else {
		listener, listenErr := net.Listen("tcp", "127.0.0.1:0")
		if listenErr != nil {
			return nil, fmt.Errorf("reserve plugin port: %w", listenErr)
		}
		address = listener.Addr().String()
		_ = listener.Close()
		cmd = exec.Command(command, manifest.Runtime.Args...)
	}
	if err != nil {
		return nil, err
	}
	if manifest.Runtime.Type == "process" {
		cmd.Dir = pluginDir
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return nil, fmt.Errorf("plugin stderr pipe: %w", err)
	}
	if manifest.Runtime.Type == "process" {
		cmd.Env = append(os.Environ(), "WEKNORA_PLUGIN_ID="+manifest.ID, "WEKNORA_PLUGIN_ADDR="+address)
	}
	var stdin io.WriteCloser
	var stdout io.ReadCloser
	if manifest.Runtime.Type == "docker" {
		stdin, err = cmd.StdinPipe()
		if err != nil {
			return nil, err
		}
		stdout, err = cmd.StdoutPipe()
		if err != nil {
			return nil, err
		}
	}
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("start plugin process: %w", err)
	}
	go logPluginStderr(manifest.ID, stderr)
	if manifest.Runtime.Type == "docker" {
		transport = &stdioConn{reader: stdout, writer: stdin}
	}

	dialTimeout := 10 * time.Second
	if manifest.Runtime.Type == "docker" {
		// Docker Desktop may need several seconds to create a container before
		// its stdio stream is ready, especially while other services start.
		dialTimeout = 30 * time.Second
	}
	dialCtx, cancel := context.WithTimeout(ctx, dialTimeout)
	defer cancel()
	var conn *grpc.ClientConn
	if transport != nil {
		conn, err = grpc.DialContext(dialCtx, "passthrough:///weknora-plugin-stdio", grpc.WithContextDialer(func(context.Context, string) (net.Conn, error) { return transport, nil }), grpc.WithTransportCredentials(insecure.NewCredentials()), grpc.WithBlock(), grpc.WithDefaultCallOptions(grpc.ForceCodec(pluginapi.JSONCodec{})))
	} else {
		conn, err = grpc.DialContext(dialCtx, address, grpc.WithTransportCredentials(insecure.NewCredentials()), grpc.WithBlock(), grpc.WithDefaultCallOptions(grpc.ForceCodec(pluginapi.JSONCodec{})))
	}
	if err != nil {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		if containerName != "" {
			_ = exec.Command("docker", "rm", "-f", containerName).Run()
		}
		return nil, fmt.Errorf("connect to plugin %s: %w", manifest.ID, err)
	}
	connector := &ProcessConnector{manifest: manifest, cmd: cmd, conn: conn, client: pluginapi.NewPluginClient(conn), containerName: containerName}
	checkCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	remote, err := connector.client.GetManifest(checkCtx, &pluginapi.ManifestRequest{})
	if err != nil {
		_ = connector.Close()
		return nil, fmt.Errorf("plugin manifest handshake: %w", err)
	}
	if remote.Manifest.ID != manifest.ID || remote.Manifest.ProtocolVersion != pluginapi.ProtocolVersion {
		_ = connector.Close()
		return nil, fmt.Errorf("plugin manifest handshake mismatch")
	}
	if err := connector.Health(checkCtx); err != nil {
		_ = connector.Close()
		return nil, fmt.Errorf("plugin health check: %w", err)
	}
	return connector, nil
}

type processWebSearchProvider struct {
	connector *ProcessConnector
	params    types.WebSearchProviderParameters
}

type processChatModel struct {
	connector *ProcessConnector
	config    *chat.ChatConfig
}

func (p *processChatModel) GetModelName() string { return p.config.ModelName }
func (p *processChatModel) GetModelID() string   { return p.config.ModelID }

func (p *processChatModel) Chat(ctx context.Context, messages []chat.Message, opts *chat.ChatOptions) (*types.ChatResponse, error) {
	request := &pluginapi.ModelChatRequest{Config: modelConfigFromChat(p.config), Options: mustJSON(opts)}
	request.Messages = make([]pluginapi.ChatMessage, 0, len(messages))
	for _, message := range messages {
		converted := pluginapi.ChatMessage{
			Role: message.Role, Content: message.Content, Name: message.Name,
			ToolCallID: message.ToolCallID, Images: message.Images,
			ReasoningContent: message.ReasoningContent,
		}
		for _, toolCall := range message.ToolCalls {
			converted.ToolCalls = append(converted.ToolCalls, pluginapi.ChatToolCall{
				ID: toolCall.ID, Type: toolCall.Type, Name: toolCall.Function.Name, Arguments: toolCall.Function.Arguments,
				ProviderMetadata: toolCall.ProviderMetadata,
			})
		}
		for _, part := range message.MultiContent {
			convertedPart := pluginapi.ChatContentPart{Type: part.Type, Text: part.Text}
			if part.ImageURL != nil {
				convertedPart.ImageURL = &pluginapi.ChatImageURL{URL: part.ImageURL.URL, Detail: part.ImageURL.Detail}
			}
			converted.MultiContent = append(converted.MultiContent, convertedPart)
		}
		request.Messages = append(request.Messages, converted)
	}
	response, err := p.connector.client.Chat(ctx, request)
	if err != nil {
		return nil, err
	}
	if response.Error != "" {
		return nil, errors.New(response.Error)
	}
	result := &types.ChatResponse{
		Content: response.Content, ReasoningContent: response.ReasoningContent,
		FinishReason: response.FinishReason,
		Usage:        types.TokenUsage{PromptTokens: response.Usage.PromptTokens, CompletionTokens: response.Usage.CompletionTokens, TotalTokens: response.Usage.TotalTokens},
	}
	for _, toolCall := range response.ToolCalls {
		result.ToolCalls = append(result.ToolCalls, types.LLMToolCall{
			ID: toolCall.ID, Type: toolCall.Type,
			Function: types.FunctionCall{Name: toolCall.Name, Arguments: toolCall.Arguments}, ProviderMetadata: toolCall.ProviderMetadata,
		})
	}
	return result, nil
}

func modelConfigFromChat(config *chat.ChatConfig) pluginapi.ModelConfig {
	return pluginapi.ModelConfig{
		Source: string(config.Source), BaseURL: config.BaseURL, ModelName: config.ModelName,
		APIKey: config.APIKey, ModelID: config.ModelID, Provider: config.Provider,
		ExtraConfig: config.ExtraConfig, CustomHeaders: config.CustomHeaders,
	}
}

func modelConfigFromProvider(config *provider.Config) pluginapi.ModelConfig {
	extra := make(map[string]string, len(config.Extra))
	for key, value := range config.Extra {
		extra[key] = fmt.Sprint(value)
	}
	return pluginapi.ModelConfig{
		BaseURL: config.BaseURL, ModelName: config.ModelName, APIKey: config.APIKey,
		ModelID: config.ModelID, Provider: string(config.Provider), ExtraConfig: extra,
	}
}

func (p *processChatModel) ChatStream(ctx context.Context, messages []chat.Message, opts *chat.ChatOptions) (<-chan types.StreamResponse, error) {
	stream := make(chan types.StreamResponse, 2)
	go func() {
		defer close(stream)
		response, err := p.Chat(ctx, messages, opts)
		if err != nil {
			stream <- types.StreamResponse{ResponseType: types.ResponseTypeError, Content: err.Error(), Done: true}
			return
		}
		if response.ReasoningContent != "" {
			stream <- types.StreamResponse{ResponseType: types.ResponseTypeThinking, Content: response.ReasoningContent}
		}
		stream <- types.StreamResponse{
			ResponseType: types.ResponseTypeAnswer, Content: response.Content,
			ToolCalls: response.ToolCalls, Usage: &response.Usage,
			FinishReason: response.FinishReason, Done: true,
		}
	}()
	return stream, nil
}

func (p *processWebSearchProvider) Name() string { return p.connector.Type() }

func (p *processWebSearchProvider) Health(ctx context.Context) error {
	return p.connector.Health(ctx)
}

func (p *processWebSearchProvider) Search(ctx context.Context, query string, maxResults int, includeDate bool) ([]*types.WebSearchResult, error) {
	response, err := p.connector.client.Search(ctx, &pluginapi.SearchRequest{
		Config: mustJSON(p.params), Query: query, MaxResults: maxResults, IncludeDate: includeDate,
	})
	if err != nil {
		return nil, err
	}
	if response.Error != "" {
		return nil, errors.New(response.Error)
	}
	results := make([]*types.WebSearchResult, 0, len(response.Results))
	for _, result := range response.Results {
		results = append(results, &types.WebSearchResult{
			Title: result.Title, URL: result.URL, Snippet: result.Snippet, Content: result.Content,
			Source: result.Source, PublishedAt: result.PublishedAt,
		})
	}
	return results, nil
}

func (c *ProcessConnector) Type() string { return c.manifest.ID }

func (c *ProcessConnector) Health(ctx context.Context) error {
	resp, err := c.client.Health(ctx, &pluginapi.HealthRequest{})
	if err != nil {
		return err
	}
	if resp.Status != "ok" {
		return fmt.Errorf("plugin health status: %s", resp.Status)
	}
	return nil
}

func (c *ProcessConnector) Parse(ctx context.Context, req *types.ReadRequest) (*types.ReadResult, error) {
	response, err := c.client.Parse(ctx, &pluginapi.ParseRequest{
		FileContent: req.FileContent, FileName: req.FileName, FileType: req.FileType,
		URL: req.URL, Title: req.Title, ParserEngineOverrides: req.ParserEngineOverrides,
	})
	if err != nil {
		return nil, err
	}
	if response.Error != "" {
		return nil, errors.New(response.Error)
	}
	return &types.ReadResult{MarkdownContent: response.MarkdownContent, Metadata: response.Metadata}, nil
}

func (c *ProcessConnector) Read(ctx context.Context, req *types.ReadRequest) (*types.ReadResult, error) {
	return c.Parse(ctx, req)
}

func (c *ProcessConnector) Validate(ctx context.Context, config *types.DataSourceConfig) error {
	resp, err := c.client.Validate(ctx, &pluginapi.ConnectorRequest{Config: mustJSON(config)})
	if err != nil {
		return err
	}
	if resp.Error != "" {
		return errors.New(resp.Error)
	}
	return nil
}

func (c *ProcessConnector) ListResources(ctx context.Context, config *types.DataSourceConfig, parentID string) ([]types.Resource, error) {
	resp, err := c.client.ListResources(ctx, &pluginapi.ConnectorRequest{Config: mustJSON(config), ParentID: parentID})
	if err != nil {
		return nil, err
	}
	if resp.Error != "" {
		return nil, errors.New(resp.Error)
	}
	out := make([]types.Resource, 0, len(resp.Resources))
	for _, resource := range resp.Resources {
		out = append(out, types.Resource{ExternalID: resource.ExternalID, Name: resource.Name, Type: resource.Type, Description: resource.Description, URL: resource.URL, ModifiedAt: resource.ModifiedAt, ParentID: resource.ParentID, HasChildren: resource.HasChildren, Metadata: resource.Metadata})
	}
	return out, nil
}

func (c *ProcessConnector) ResolveResourceAncestors(ctx context.Context, config *types.DataSourceConfig, resourceIDs []string) ([]string, error) {
	resp, err := c.client.ResolveResourceAncestors(ctx, &pluginapi.ConnectorRequest{Config: mustJSON(config), ResourceIDs: resourceIDs})
	if err != nil {
		return nil, err
	}
	if resp.Error != "" {
		return nil, errors.New(resp.Error)
	}
	return resp.ResourceIDs, nil
}

func (c *ProcessConnector) FetchAll(ctx context.Context, config *types.DataSourceConfig, resourceIDs []string) ([]types.FetchedItem, error) {
	resp, err := c.client.FetchAll(ctx, &pluginapi.ConnectorRequest{Config: mustJSON(config), ResourceIDs: resourceIDs})
	return decodeItems(resp, err)
}

func (c *ProcessConnector) FetchIncremental(ctx context.Context, config *types.DataSourceConfig, cursor *types.SyncCursor) ([]types.FetchedItem, *types.SyncCursor, error) {
	resp, err := c.client.FetchIncremental(ctx, &pluginapi.ConnectorRequest{Config: mustJSON(config), Cursor: mustJSON(cursor)})
	items, err := decodeItems(resp, err)
	if err != nil {
		return nil, nil, err
	}
	var next *types.SyncCursor
	if len(resp.NextCursor) > 0 {
		next = new(types.SyncCursor)
		if err := json.Unmarshal(resp.NextCursor, next); err != nil {
			return nil, nil, err
		}
	}
	return items, next, nil
}

func decodeItems(resp *pluginapi.FetchResponse, err error) ([]types.FetchedItem, error) {
	if err != nil {
		return nil, err
	}
	if resp == nil {
		return nil, errors.New("plugin returned empty response")
	}
	if resp.Error != "" {
		return nil, errors.New(resp.Error)
	}
	out := make([]types.FetchedItem, 0, len(resp.Items))
	for _, item := range resp.Items {
		out = append(out, types.FetchedItem{ExternalID: item.ExternalID, Title: item.Title, Content: item.Content, ContentType: item.ContentType, FileName: item.FileName, URL: item.URL, UpdatedAt: item.UpdatedAt, Metadata: item.Metadata, IsDeleted: item.IsDeleted, SourceResourceID: item.SourceResourceID})
	}
	return out, nil
}

func (c *ProcessConnector) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	var errs []error
	if c.conn != nil {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		_, _ = c.client.Shutdown(shutdownCtx, &pluginapi.ShutdownRequest{})
		cancel()
		errs = append(errs, c.conn.Close())
		c.conn = nil
	}
	if c.cmd != nil && c.cmd.Process != nil {
		if err := c.cmd.Process.Kill(); err != nil && !errors.Is(err, os.ErrProcessDone) {
			errs = append(errs, err)
		}
		_ = c.cmd.Wait()
	}
	if c.containerName != "" {
		_ = exec.Command("docker", "rm", "-f", c.containerName).Run()
	}
	return errors.Join(errs...)
}

func newDockerCommand(manifest pluginapi.Manifest, pluginDir string) (*exec.Cmd, string, error) {
	containerName := "weknora-plugin-" + strings.NewReplacer("/", "-", ":", "-", "_", "-").Replace(manifest.ID) + "-" + strconv.FormatInt(time.Now().UnixNano(), 10)
	args := []string{"run", "--rm", "-i", "--name", containerName, "--user", "65532:65532", "--read-only", "--cap-drop", "ALL", "--security-opt", "no-new-privileges", "--pids-limit", "128"}
	if manifest.Permissions.AllowNetwork {
		args = append(args, "--network", "bridge")
	} else {
		args = append(args, "--network", "none")
	}
	args = append(args, "-e", "WEKNORA_PLUGIN_STDIO=1", "-e", "WEKNORA_PLUGIN_ID="+manifest.ID)
	for i, path := range manifest.Permissions.ReadPaths {
		if !isHostAbsolutePath(path) {
			path = filepath.Join(pluginDir, path)
		}
		args = append(args, "--mount", fmt.Sprintf("type=bind,src=%s,dst=/weknora/inputs/%d,readonly", path, i))
	}
	args = append(args, manifest.Runtime.Image)
	if manifest.Runtime.Command != "" {
		args = append(args, manifest.Runtime.Command)
	}
	args = append(args, manifest.Runtime.Args...)
	return exec.Command("docker", args...), containerName, nil
}

func isHostAbsolutePath(path string) bool {
	if filepath.IsAbs(path) {
		return true
	}
	return len(path) >= 3 && ((path[0] >= 'a' && path[0] <= 'z') || (path[0] >= 'A' && path[0] <= 'Z')) && path[1] == ':' && (path[2] == '\\' || path[2] == '/')
}

type stdioConn struct {
	reader io.ReadCloser
	writer io.WriteCloser
	once   sync.Once
}

func (c *stdioConn) Read(p []byte) (int, error)  { return c.reader.Read(p) }
func (c *stdioConn) Write(p []byte) (int, error) { return c.writer.Write(p) }
func (c *stdioConn) Close() error {
	var err error
	c.once.Do(func() { _ = c.writer.Close(); err = c.reader.Close() })
	return err
}
func (c *stdioConn) LocalAddr() net.Addr              { return pluginAddr("stdio") }
func (c *stdioConn) RemoteAddr() net.Addr             { return pluginAddr("stdio") }
func (c *stdioConn) SetDeadline(time.Time) error      { return nil }
func (c *stdioConn) SetReadDeadline(time.Time) error  { return nil }
func (c *stdioConn) SetWriteDeadline(time.Time) error { return nil }

type pluginAddr string

func (a pluginAddr) Network() string { return string(a) }
func (a pluginAddr) String() string  { return string(a) }

func logPluginStderr(id string, stderr io.ReadCloser) {
	scanner := bufio.NewScanner(stderr)
	for scanner.Scan() {
		logger.Warnf(context.Background(), "plugin %s stderr: %s", id, scanner.Text())
	}
	if err := scanner.Err(); err != nil {
		logger.Warnf(context.Background(), "plugin %s stderr read failed: %v", id, err)
	}
}

func mustJSON(v interface{}) json.RawMessage {
	if v == nil {
		return nil
	}
	b, _ := json.Marshal(v)
	return b
}

func contains(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}
