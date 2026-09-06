package web_search

import (
	"context"
	"fmt"
	"sort"
	"sync"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
)

// ProviderFactory creates a new web search provider instance from parameters.
type ProviderFactory func(params types.WebSearchProviderParameters) (interfaces.WebSearchProvider, error)

type ProviderStatus struct {
	Type    string `json:"type"`
	Healthy bool   `json:"healthy"`
	Error   string `json:"error,omitempty"`
}

// Registry manages web search provider type registrations.
// It maps provider type IDs (e.g., "bing", "google") to their factory functions.
// Instances are created on-demand with tenant-specific parameters.
type Registry struct {
	factories    map[string]ProviderFactory
	externalInfo map[string]types.WebSearchProviderTypeInfo
	mu           sync.RWMutex
}

// NewRegistry creates a new web search provider registry
func NewRegistry() *Registry {
	return &Registry{
		factories:    make(map[string]ProviderFactory),
		externalInfo: make(map[string]types.WebSearchProviderTypeInfo),
	}
}

// Register registers a provider type factory by ID
func (r *Registry) Register(id string, factory ProviderFactory) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.factories[id] = factory
}

// RegisterExternal registers an external provider and its UI metadata.
func (r *Registry) RegisterExternal(info types.WebSearchProviderTypeInfo, factory ProviderFactory) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, exists := r.factories[info.ID]; exists {
		return fmt.Errorf("web search provider type %s already registered", info.ID)
	}
	info.External = true
	r.factories[info.ID] = factory
	r.externalInfo[info.ID] = info
	return nil
}

// UnregisterExternal removes a provider previously added by RegisterExternal.
func (r *Registry) UnregisterExternal(id string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, external := r.externalInfo[id]; !external {
		return
	}
	delete(r.externalInfo, id)
	delete(r.factories, id)
}

// ProviderTypes returns built-in and external provider metadata.
func (r *Registry) ProviderTypes() []types.WebSearchProviderTypeInfo {
	r.mu.RLock()
	external := make([]types.WebSearchProviderTypeInfo, 0, len(r.externalInfo))
	for _, info := range r.externalInfo {
		external = append(external, info)
	}
	r.mu.RUnlock()
	sort.Slice(external, func(i, j int) bool { return external[i].ID < external[j].ID })
	return append(types.GetWebSearchProviderTypes(), external...)
}

// ProviderType returns metadata for a registered provider type.
func (r *Registry) ProviderType(id string) (types.WebSearchProviderTypeInfo, bool) {
	for _, info := range r.ProviderTypes() {
		if info.ID == id {
			return info, true
		}
	}
	return types.WebSearchProviderTypeInfo{}, false
}

// ExternalStatuses checks the current health of loaded external providers.
func (r *Registry) ExternalStatuses(ctx context.Context) []ProviderStatus {
	r.mu.RLock()
	factories := make(map[string]ProviderFactory, len(r.externalInfo))
	for id := range r.externalInfo {
		factories[id] = r.factories[id]
	}
	r.mu.RUnlock()

	statuses := make([]ProviderStatus, 0, len(factories))
	for id, factory := range factories {
		status := ProviderStatus{Type: id, Healthy: true}
		provider, err := factory(types.WebSearchProviderParameters{})
		if err == nil {
			if checker, ok := provider.(interface{ Health(context.Context) error }); ok {
				err = checker.Health(ctx)
			}
		}
		if err != nil {
			status.Healthy = false
			status.Error = err.Error()
		}
		statuses = append(statuses, status)
	}
	sort.Slice(statuses, func(i, j int) bool { return statuses[i].Type < statuses[j].Type })
	return statuses
}

// CreateProvider creates a provider instance by type with the given parameters.
func (r *Registry) CreateProvider(providerType string, params types.WebSearchProviderParameters) (interfaces.WebSearchProvider, error) {
	r.mu.RLock()
	factory, ok := r.factories[providerType]
	r.mu.RUnlock()
	if !ok {
		return nil, fmt.Errorf("web search provider type %s not registered", providerType)
	}
	return factory(params)
}
