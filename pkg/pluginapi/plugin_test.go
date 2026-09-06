package pluginapi

import (
	"context"
	"encoding/json"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/test/bufconn"
)

func TestVersionCompatible(t *testing.T) {
	if !VersionCompatible("0.6.3", ">=0.6.0 <1.0.0") {
		t.Fatal("expected version to be compatible")
	}
	if VersionCompatible("1.0.0", ">=0.6.0 <1.0.0") {
		t.Fatal("expected version to be incompatible")
	}
}

func TestValidateManifestRequiresRuntime(t *testing.T) {
	err := ValidateManifest(Manifest{ID: "test", Name: "Test", Version: "1.0.0", ProtocolVersion: ProtocolVersion, ExtensionTypes: []string{"datasource"}, WeKnoraVersion: "*"})
	if err == nil {
		t.Fatal("expected runtime validation error")
	}
}

func TestValidateManifestRejectsInvalidExtensionTypes(t *testing.T) {
	base := Manifest{ID: "test", Name: "Test", Version: "1.0.0", ProtocolVersion: ProtocolVersion, WeKnoraVersion: "*", Runtime: Runtime{Type: "docker", Image: "test"}}
	for _, extensionTypes := range [][]string{{"datasource", ""}, {"datasource", "datasource"}} {
		base.ExtensionTypes = extensionTypes
		if err := ValidateManifest(base); err == nil {
			t.Fatalf("expected invalid extension types to fail: %#v", extensionTypes)
		}
	}
}

func TestValidateManifestRequiresModelTypes(t *testing.T) {
	err := ValidateManifest(Manifest{
		ID: "model", Name: "Model", Version: "1.0.0", ProtocolVersion: ProtocolVersion,
		ExtensionTypes: []string{"model_provider"}, WeKnoraVersion: "*",
		Runtime: Runtime{Type: "docker", Image: "test"},
	})
	if err == nil {
		t.Fatal("expected model_types validation error")
	}
}

type testConnector struct{}

func (testConnector) Validate(context.Context, json.RawMessage) error { return nil }
func (testConnector) ListResources(context.Context, json.RawMessage, string) ([]Resource, error) {
	return []Resource{{ExternalID: "one", Name: "One"}}, nil
}
func (testConnector) ResolveResourceAncestors(context.Context, json.RawMessage, []string) ([]string, error) {
	return nil, nil
}
func (testConnector) FetchAll(context.Context, json.RawMessage, []string) ([]FetchedItem, error) {
	return []FetchedItem{{ExternalID: "one", Content: []byte("content")}}, nil
}
func (testConnector) FetchIncremental(context.Context, json.RawMessage, json.RawMessage) ([]FetchedItem, json.RawMessage, error) {
	return nil, nil, nil
}

type testParser struct{}

func (testParser) Parse(context.Context, *ParseRequest) (*ParseResponse, error) {
	return &ParseResponse{MarkdownContent: "# parsed"}, nil
}

type testSearchProvider struct{}

func (testSearchProvider) Search(context.Context, json.RawMessage, string, int, bool) ([]SearchResult, error) {
	return []SearchResult{{Title: "result", URL: "https://example.com"}}, nil
}

type testModelProvider struct{}

func (testModelProvider) ValidateConfig(context.Context, ModelConfig) error { return nil }

func (testModelProvider) Chat(context.Context, *ModelChatRequest) (*ModelChatResponse, error) {
	return &ModelChatResponse{Content: "model result", FinishReason: "stop"}, nil
}

func TestGRPCJSONRoundTrip(t *testing.T) {
	const bufferSize = 1024 * 1024
	listener := bufconn.Listen(bufferSize)
	server := grpc.NewServer(grpc.ForceServerCodec(JSONCodec{}))
	RegisterPluginServer(server, &connectorServer{
		manifest:  Manifest{ID: "test", Name: "Test", Version: "1.0.0", ProtocolVersion: ProtocolVersion, ExtensionTypes: []string{"datasource"}, WeKnoraVersion: "*", Runtime: Runtime{Type: "process", Command: "test"}},
		connector: testConnector{},
	})
	go func() { _ = server.Serve(listener) }()
	defer server.Stop()

	conn, err := grpc.DialContext(context.Background(), "bufnet", grpc.WithContextDialer(func(context.Context, string) (net.Conn, error) { return listener.Dial() }), grpc.WithTransportCredentials(insecure.NewCredentials()), grpc.WithDefaultCallOptions(grpc.ForceCodec(JSONCodec{})))
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()

	client := NewPluginClient(conn)
	response, err := client.FetchAll(context.Background(), &ConnectorRequest{})
	if err != nil {
		t.Fatal(err)
	}
	if len(response.Items) != 1 || string(response.Items[0].Content) != "content" {
		t.Fatalf("unexpected response: %#v", response)
	}
}

func TestGRPCParserRoundTrip(t *testing.T) {
	listener := bufconn.Listen(1024 * 1024)
	server := grpc.NewServer(grpc.ForceServerCodec(JSONCodec{}))
	RegisterPluginServer(server, &connectorServer{
		manifest: Manifest{ID: "parser", Name: "Parser", Version: "1.0.0", ProtocolVersion: ProtocolVersion, ExtensionTypes: []string{"document_parser"}, WeKnoraVersion: "*", Runtime: Runtime{Type: "process", Command: "test"}},
		parser:   testParser{},
	})
	go func() { _ = server.Serve(listener) }()
	defer server.Stop()

	conn, err := grpc.DialContext(context.Background(), "bufnet", grpc.WithContextDialer(func(context.Context, string) (net.Conn, error) { return listener.Dial() }), grpc.WithTransportCredentials(insecure.NewCredentials()), grpc.WithDefaultCallOptions(grpc.ForceCodec(JSONCodec{})))
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()

	response, err := NewPluginClient(conn).Parse(context.Background(), &ParseRequest{FileName: "a.md"})
	if err != nil || response.MarkdownContent != "# parsed" {
		t.Fatalf("unexpected parser response: %#v, %v", response, err)
	}
}

func TestGRPCWebSearchRoundTrip(t *testing.T) {
	listener := bufconn.Listen(1024 * 1024)
	server := grpc.NewServer(grpc.ForceServerCodec(JSONCodec{}))
	RegisterPluginServer(server, &connectorServer{
		manifest: Manifest{ID: "search", Name: "Search", Version: "1.0.0", ProtocolVersion: ProtocolVersion, ExtensionTypes: []string{"web_search"}, WeKnoraVersion: "*", Runtime: Runtime{Type: "process", Command: "test"}},
		search:   testSearchProvider{},
	})
	go func() { _ = server.Serve(listener) }()
	defer server.Stop()

	conn, err := grpc.DialContext(context.Background(), "bufnet", grpc.WithContextDialer(func(context.Context, string) (net.Conn, error) { return listener.Dial() }), grpc.WithTransportCredentials(insecure.NewCredentials()), grpc.WithDefaultCallOptions(grpc.ForceCodec(JSONCodec{})))
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()

	response, err := NewPluginClient(conn).Search(context.Background(), &SearchRequest{Query: "test", MaxResults: 1})
	if err != nil || len(response.Results) != 1 || response.Results[0].Title != "result" {
		t.Fatalf("unexpected search response: %#v, %v", response, err)
	}
}

func TestGRPCModelRoundTrip(t *testing.T) {
	listener := bufconn.Listen(1024 * 1024)
	server := grpc.NewServer(grpc.ForceServerCodec(JSONCodec{}))
	RegisterPluginServer(server, &connectorServer{
		manifest: Manifest{ID: "model", Name: "Model", Version: "1.0.0", ProtocolVersion: ProtocolVersion, ExtensionTypes: []string{"model_provider"}, ModelTypes: []string{"chat"}, WeKnoraVersion: "*", Runtime: Runtime{Type: "process", Command: "test"}},
		model:    testModelProvider{},
	})
	go func() { _ = server.Serve(listener) }()
	defer server.Stop()

	conn, err := grpc.DialContext(context.Background(), "bufnet", grpc.WithContextDialer(func(context.Context, string) (net.Conn, error) { return listener.Dial() }), grpc.WithTransportCredentials(insecure.NewCredentials()), grpc.WithDefaultCallOptions(grpc.ForceCodec(JSONCodec{})))
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()

	response, err := NewPluginClient(conn).Chat(context.Background(), &ModelChatRequest{})
	if err != nil || response.Content != "model result" {
		t.Fatalf("unexpected model response: %#v, %v", response, err)
	}
	validation, err := NewPluginClient(conn).ValidateModelConfig(context.Background(), &ModelValidateRequest{})
	if err != nil || validation.Error != "" {
		t.Fatalf("unexpected validation response: %#v, %v", validation, err)
	}
}

func TestDockerStdioRoundTrip(t *testing.T) {
	image := os.Getenv("WEKNORA_TEST_PLUGIN_IMAGE")
	if image == "" {
		t.Skip("WEKNORA_TEST_PLUGIN_IMAGE is not set")
	}
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "a.md"), []byte("a"), 0600); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("docker", "run", "--rm", "-i", "--network", "none",
		"--mount", "type=bind,src="+root+",dst=/inputs,readonly",
		"-e", "WEKNORA_PLUGIN_STDIO=1", image)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	transport := &stdioConn{reader: stdout, writer: stdin}
	dialCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	conn, err := grpc.DialContext(dialCtx, "stdio",
		grpc.WithContextDialer(func(context.Context, string) (net.Conn, error) { return transport, nil }),
		grpc.WithTransportCredentials(insecure.NewCredentials()), grpc.WithBlock(),
		grpc.WithDefaultCallOptions(grpc.ForceCodec(JSONCodec{})))
	if err != nil {
		_ = cmd.Process.Kill()
		t.Fatal(err)
	}
	client := NewPluginClient(conn)
	config := json.RawMessage(`{"settings":{"root_path":"/inputs"}}`)
	response, err := client.FetchIncremental(context.Background(), &ConnectorRequest{Config: config})
	if err != nil {
		t.Fatal(err)
	}
	if len(response.Items) != 1 || response.Items[0].ExternalID != "a.md" {
		t.Fatalf("unexpected plugin response: %#v", response)
	}
	_, _ = client.Shutdown(context.Background(), &ShutdownRequest{})
	_ = conn.Close()
	_ = cmd.Wait()
}
