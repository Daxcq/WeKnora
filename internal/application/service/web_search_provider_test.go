package service

import (
	"context"
	"testing"

	infra_web_search "github.com/Tencent/WeKnora/internal/infrastructure/web_search"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
)

type recordingWebSearchProviderRepository struct {
	interfaces.WebSearchProviderRepository
	created bool
}

func (r *recordingWebSearchProviderRepository) Create(context.Context, *types.WebSearchProviderEntity) error {
	r.created = true
	return nil
}

func TestCreateExternalWebSearchProvider(t *testing.T) {
	registry := infra_web_search.NewRegistry()
	if err := registry.RegisterExternal(types.WebSearchProviderTypeInfo{ID: "external", Name: "External", RequiresAPIKey: true}, nil); err != nil {
		t.Fatal(err)
	}
	repo := &recordingWebSearchProviderRepository{}
	service := NewWebSearchProviderService(repo, registry)
	provider := &types.WebSearchProviderEntity{TenantID: 1, Provider: "external", Parameters: types.WebSearchProviderParameters{APIKey: "key"}}

	if err := service.CreateProvider(context.Background(), provider); err != nil {
		t.Fatal(err)
	}
	if !repo.created {
		t.Fatal("external provider was not persisted")
	}
}
