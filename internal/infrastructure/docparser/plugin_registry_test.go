package docparser

import (
	"context"
	"errors"
	"testing"

	"github.com/Tencent/WeKnora/internal/types"
)

type testExternalParser struct{}

func (testExternalParser) Read(context.Context, *types.ReadRequest) (*types.ReadResult, error) {
	return &types.ReadResult{MarkdownContent: "ok"}, nil
}

type unhealthyExternalParser struct{}

func (unhealthyExternalParser) Read(context.Context, *types.ReadRequest) (*types.ReadResult, error) {
	return nil, errors.New("unavailable")
}

func (unhealthyExternalParser) Health(context.Context) error { return errors.New("parser is down") }

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
	engines := ExternalParserEngines(context.Background())
	if len(engines) != 1 || engines[0].Name != name {
		t.Fatalf("unexpected parser engines: %+v", engines)
	}
}

func TestExternalParserEnginesChecksHealth(t *testing.T) {
	name := "unhealthy-external-parser"
	UnregisterExternalParser(name)
	defer UnregisterExternalParser(name)
	if err := RegisterExternalParser(name, unhealthyExternalParser{}, types.ParserEngineInfo{Name: name, Available: true}); err != nil {
		t.Fatal(err)
	}
	engines := ExternalParserEngines(context.Background())
	if len(engines) != 1 || engines[0].Available || engines[0].UnavailableReason != "parser is down" {
		t.Fatalf("health check did not update parser status: %+v", engines)
	}
}
