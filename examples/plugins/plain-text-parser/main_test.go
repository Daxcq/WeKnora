package main

import (
	"context"
	"testing"

	"github.com/Tencent/WeKnora/pkg/pluginapi"
)

func TestParser(t *testing.T) {
	result, err := (parser{}).Parse(context.Background(), &pluginapi.ParseRequest{
		FileType: "md", FileContent: []byte("# hello"),
	})
	if err != nil || result.MarkdownContent != "# hello" {
		t.Fatalf("unexpected parse result: %#v, %v", result, err)
	}
}
