package docparser

import (
	"context"
	"fmt"
	"sync"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
)

type externalParser struct {
	reader interfaces.DocReader
	info   types.ParserEngineInfo
}

var externalParsers = struct {
	sync.RWMutex
	items map[string]externalParser
}{items: make(map[string]externalParser)}

func RegisterExternalParser(name string, reader interfaces.DocReader, info types.ParserEngineInfo) error {
	if name == "" || reader == nil {
		return fmt.Errorf("external parser requires name and reader")
	}
	externalParsers.Lock()
	defer externalParsers.Unlock()
	if _, exists := externalParsers.items[name]; exists {
		return fmt.Errorf("parser engine %s already registered", name)
	}
	externalParsers.items[name] = externalParser{reader: reader, info: info}
	return nil
}

func UnregisterExternalParser(name string) {
	externalParsers.Lock()
	delete(externalParsers.items, name)
	externalParsers.Unlock()
}

func ExternalParser(name string) interfaces.DocReader {
	externalParsers.RLock()
	parser := externalParsers.items[name]
	externalParsers.RUnlock()
	return parser.reader
}

func ExternalParserEngines(ctx context.Context) []types.ParserEngineInfo {
	externalParsers.RLock()
	defer externalParsers.RUnlock()
	result := make([]types.ParserEngineInfo, 0, len(externalParsers.items))
	for _, parser := range externalParsers.items {
		info := parser.info
		if checker, ok := parser.reader.(interface{ Health(context.Context) error }); ok {
			if err := checker.Health(ctx); err != nil {
				info.Available = false
				info.UnavailableReason = err.Error()
			}
		}
		result = append(result, info)
	}
	return result
}
