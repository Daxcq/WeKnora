package docparser

import (
	"context"
	"testing"

	"github.com/Tencent/WeKnora/internal/types"
)

type testExternalParser struct{}

func (testExternalParser) Read(context.Context, *types.ReadRequest) (*types.ReadResult, error) {
	return &types.ReadResult{MarkdownContent: "ok"}, nil
}

func TestExternalParserRegistry(t *testing.T) {
	name := "test-external-parser"
	UnregisterExternalParser(name)
	reader := testExternalParser{}
	if err := RegisterExternalParser(name, reader, types.ParserEngineInfo{Name: name, FileTypes: []string{"md"}, Available: true}); err != nil {
		t.Fatal(err)
	}
	defer UnregisterExternalParser(name)
	if ExternalParser(name) != reader {
		t.Fatal("registered parser was not returned")
	}
	engines := ExternalParserEngines()
	if len(engines) != 1 || engines[0].Name != name {
		t.Fatalf("unexpected parser engines: %+v", engines)
	}
}
