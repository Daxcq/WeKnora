package main

import (
	"context"
	"encoding/json"
	"log"

	"github.com/Tencent/WeKnora/pkg/pluginapi"
)

type connector struct{}

func (connector) Validate(context.Context, json.RawMessage) error { return nil }
func (connector) ListResources(context.Context, json.RawMessage, string) ([]pluginapi.Resource, error) {
	return []pluginapi.Resource{{ExternalID: "example", Name: "Example", Type: "document"}}, nil
}
func (connector) ResolveResourceAncestors(context.Context, json.RawMessage, []string) ([]string, error) {
	return nil, nil
}
func (connector) FetchAll(context.Context, json.RawMessage, []string) ([]pluginapi.FetchedItem, error) {
	return []pluginapi.FetchedItem{{ExternalID: "example", Title: "Example", FileName: "example.md", ContentType: "text/markdown", Content: []byte("# Hello from a plugin")}}, nil
}
func (connector) FetchIncremental(context.Context, json.RawMessage, json.RawMessage) ([]pluginapi.FetchedItem, json.RawMessage, error) {
	return nil, json.RawMessage(`{"version":1}`), nil
}

func main() {
	manifest := pluginapi.Manifest{
		ID: "replace-me", Name: "Replace Me", Version: "0.1.0",
		ProtocolVersion: pluginapi.ProtocolVersion, ExtensionTypes: []string{"datasource"},
		WeKnoraVersion: ">=0.6.0 <1.0.0",
		Permissions:    pluginapi.Permissions{AllowNetwork: false},
		Runtime:        pluginapi.Runtime{Type: "docker", Image: "replace-me:dev"},
	}
	if err := pluginapi.Serve(manifest, connector{}); err != nil {
		log.Fatal(err)
	}
}
