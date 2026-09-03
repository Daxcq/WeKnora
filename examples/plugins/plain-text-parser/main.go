package main

import (
	"context"
	"fmt"
	"log"
	"strings"

	"github.com/Tencent/WeKnora/pkg/pluginapi"
)

type parser struct{}

func (parser) Parse(_ context.Context, req *pluginapi.ParseRequest) (*pluginapi.ParseResponse, error) {
	fileType := strings.TrimPrefix(strings.ToLower(req.FileType), ".")
	if fileType != "md" && fileType != "markdown" && fileType != "txt" && fileType != "text" {
		return nil, fmt.Errorf("unsupported file type: %s", req.FileType)
	}
	return &pluginapi.ParseResponse{
		MarkdownContent: string(req.FileContent),
		Metadata:        map[string]string{"parser": "plain-text-parser"},
	}, nil
}

func main() {
	manifest := pluginapi.Manifest{
		ID: "plain-text-parser", Name: "Plain Text Parser", Version: "0.1.0",
		ProtocolVersion: pluginapi.ProtocolVersion, ExtensionTypes: []string{"document_parser"},
		FileTypes:      []string{"md", "markdown", "txt", "text"},
		WeKnoraVersion: ">=0.6.0 <1.0.0",
		Permissions:    pluginapi.Permissions{AllowNetwork: false},
		Runtime:        pluginapi.Runtime{Type: "docker", Image: "weknora-plugin-plain-text-parser:dev"},
	}
	if err := pluginapi.Serve(manifest, parser{}); err != nil {
		log.Fatal(err)
	}
}
