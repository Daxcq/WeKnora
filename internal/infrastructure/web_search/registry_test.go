package web_search

import (
	"context"
	"testing"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
)

type registryTestProvider struct{}

func (registryTestProvider) Name() string { return "external-search" }
func (registryTestProvider) Search(context.Context, string, int, bool) ([]*types.WebSearchResult, error) {
	return nil, nil
}

func TestRegisterExternalProvider(t *testing.T) {
	registry := NewRegistry()
	err := registry.RegisterExternal(types.WebSearchProviderTypeInfo{ID: "external-search", Name: "External Search"}, func(types.WebSearchProviderParameters) (interfaces.WebSearchProvider, error) {
		return registryTestProvider{}, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	provider, err := registry.CreateProvider("external-search", types.WebSearchProviderParameters{})
	if err != nil || provider.Name() != "external-search" {
		t.Fatalf("unexpected provider: %#v, %v", provider, err)
	}
	types := registry.ProviderTypes()
	if got := types[len(types)-1]; got.ID != "external-search" || !got.External {
		t.Fatalf("unexpected metadata: %+v", got)
	}
}
