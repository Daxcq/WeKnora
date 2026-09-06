package datasource

import (
	"context"
	"sort"
	"sync"

	"github.com/Tencent/WeKnora/internal/types"
)

// Connector is the interface that all external data source connectors must implement.
// Each connector (Feishu, Notion, Confluence, etc.) provides an implementation of this interface.
type Connector interface {
	// Type returns the connector type identifier (e.g., "feishu", "notion")
	Type() string

	// Validate verifies that the provided configuration is valid by testing connectivity
	// and checking credentials. Returns error if validation fails.
	Validate(ctx context.Context, config *types.DataSourceConfig) error

	// ListResources lists available resources that can be synced (documents, spaces, folders, etc.)
	// Returns a list of Resource objects that the user can select for syncing.
	//
	// parentID controls lazy (on-demand) loading of hierarchical resources:
	//   - parentID == "" → return the top-level resources (e.g. Feishu wiki spaces).
	//   - parentID != "" → return only the direct children of that resource.
	// Connectors whose listing is already flat or returns the full tree in a single
	// call may ignore parentID for the root call and return an empty slice for any
	// non-empty parentID.
	ListResources(ctx context.Context, config *types.DataSourceConfig, parentID string) ([]types.Resource, error)

	// ResolveResourceAncestors resolves, for each of the given resource IDs, the
	// ExternalIDs of every ancestor whose direct children must be loaded so a
	// lazily-loaded picker can reveal a pre-existing (possibly deeply nested)
	// selection. The returned set is deduplicated and unordered.
	//
	// It exists so connectors that load their tree one level at a time (e.g. the
	// Feishu wiki) can expose, in O(depth) per selection, the path back to the
	// root without re-traversing the whole tree. Connectors that already return
	// the full tree (Notion) or a flat list (Yuque) have nothing to reveal and
	// return an empty slice.
	ResolveResourceAncestors(
		ctx context.Context, config *types.DataSourceConfig, resourceIDs []string,
	) ([]string, error)

	// FetchAll performs a full sync of the specified resources.
	// Returns all items from the given resource IDs.
	FetchAll(ctx context.Context, config *types.DataSourceConfig, resourceIDs []string) ([]types.FetchedItem, error)

	// FetchIncremental performs an incremental sync based on the provided cursor.
	// Returns items that have changed since the last sync, a new cursor for the next sync,
	// and an error if the operation fails.
	FetchIncremental(ctx context.Context, config *types.DataSourceConfig, cursor *types.SyncCursor) ([]types.FetchedItem, *types.SyncCursor, error)
}

// ConnectorRegistry manages the registration and lookup of available connectors
type ConnectorRegistry struct {
	mu         sync.RWMutex
	connectors map[string]Connector
	disabled   map[string]bool
}

const DisabledPluginsSettingKey = "plugins.disabled"

// ConnectorStatus is the runtime state of a registered connector.
type ConnectorStatus struct {
	Type    string `json:"type"`
	Enabled bool   `json:"enabled"`
	Healthy bool   `json:"healthy"`
	Error   string `json:"error,omitempty"`
}

// NewConnectorRegistry creates a new connector registry
func NewConnectorRegistry() *ConnectorRegistry {
	return &ConnectorRegistry{
		connectors: make(map[string]Connector),
		disabled:   make(map[string]bool),
	}
}

// Register registers a connector with the registry
func (r *ConnectorRegistry) Register(connector Connector) error {
	if connector == nil {
		return ErrConnectorNil
	}
	if connector.Type() == "" {
		return ErrConnectorTypeEmpty
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, exists := r.connectors[connector.Type()]; exists {
		return ErrConnectorDuplicate
	}
	r.connectors[connector.Type()] = connector
	return nil
}

// Get retrieves a connector by type
func (r *ConnectorRegistry) Get(connectorType string) (Connector, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	connector, exists := r.connectors[connectorType]
	if !exists {
		return nil, ErrConnectorNotFound
	}
	if r.disabled[connectorType] {
		return nil, ErrConnectorDisabled
	}
	return connector, nil
}

// SetEnabled changes whether a registered connector can receive new requests.
func (r *ConnectorRegistry) SetEnabled(connectorType string, enabled bool) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, exists := r.connectors[connectorType]; !exists {
		return ErrConnectorNotFound
	}
	r.disabled[connectorType] = !enabled
	return nil
}

// Health checks a connector through the same registry used for sync requests.
// In-process connectors are healthy when registered and enabled; external
// connectors additionally expose their gRPC health RPC.
func (r *ConnectorRegistry) Health(ctx context.Context, connectorType string) error {
	r.mu.RLock()
	connector, exists := r.connectors[connectorType]
	r.mu.RUnlock()
	if !exists {
		return ErrConnectorNotFound
	}
	if checker, ok := connector.(interface{ Health(context.Context) error }); ok {
		return checker.Health(ctx)
	}
	return nil
}

// Statuses returns a deterministic snapshot for all registered connectors.
// Health is checked even for disabled connectors so an administrator can see
// whether re-enabling one would succeed.
func (r *ConnectorRegistry) Statuses(ctx context.Context) []ConnectorStatus {
	r.mu.RLock()
	connectors := make(map[string]Connector, len(r.connectors))
	enabled := make(map[string]bool, len(r.connectors))
	for connectorType, connector := range r.connectors {
		connectors[connectorType] = connector
		enabled[connectorType] = !r.disabled[connectorType]
	}
	r.mu.RUnlock()

	statuses := make([]ConnectorStatus, 0, len(connectors))
	for connectorType, connector := range connectors {
		status := ConnectorStatus{Type: connectorType, Enabled: enabled[connectorType], Healthy: true}
		if checker, ok := connector.(interface{ Health(context.Context) error }); ok {
			if err := checker.Health(ctx); err != nil {
				status.Healthy = false
				status.Error = err.Error()
			}
		}
		statuses = append(statuses, status)
	}
	sort.Slice(statuses, func(i, j int) bool { return statuses[i].Type < statuses[j].Type })
	return statuses
}

// List returns all registered connector types
func (r *ConnectorRegistry) List() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	types := make([]string, 0, len(r.connectors))
	for t := range r.connectors {
		if r.disabled[t] {
			continue
		}
		types = append(types, t)
	}
	return types
}

// DisabledTypes returns a deterministic snapshot of explicitly disabled connectors.
func (r *ConnectorRegistry) DisabledTypes() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	result := make([]string, 0, len(r.disabled))
	for connectorType, disabled := range r.disabled {
		if disabled {
			result = append(result, connectorType)
		}
	}
	sort.Strings(result)
	return result
}

// ConnectorMetadata provides metadata about available connectors
type ConnectorMetadata struct {
	Type         string                 `json:"type"`
	Name         string                 `json:"name"`
	Description  string                 `json:"description"`
	Icon         string                 `json:"icon,omitempty"`
	Priority     int                    `json:"priority"`     // Priority order for UI display (lower = higher priority)
	AuthType     string                 `json:"auth_type"`    // "oauth2", "api_key", "token", etc.
	Capabilities []string               `json:"capabilities"` // "incremental", "webhook", "deletion_sync", etc.
	Config       []ConnectorConfigField `json:"config,omitempty"`
	External     bool                   `json:"external,omitempty"`
	Permissions  *ConnectorPermissions  `json:"permissions,omitempty"`
}

type ConnectorPermissions struct {
	AllowNetwork bool     `json:"allow_network"`
	ReadPaths    []string `json:"read_paths,omitempty"`
}

type ConnectorConfigField struct {
	Name        string `json:"name"`
	Type        string `json:"type"`
	Required    bool   `json:"required,omitempty"`
	Secret      bool   `json:"secret,omitempty"`
	Description string `json:"description,omitempty"`
}

// GetConnectorMetadata returns metadata for all available connectors
// This is used by the frontend to display connector options
var ConnectorMetadataRegistry = map[string]ConnectorMetadata{
	types.ConnectorTypeFeishu: {
		Type:         types.ConnectorTypeFeishu,
		Name:         "Feishu (飞书)",
		Description:  "Sync documents, wikis, and content from Feishu",
		Priority:     0,
		AuthType:     "oauth2",
		Capabilities: []string{"incremental", "deletion_sync"},
	},
	types.ConnectorTypeNotion: {
		Type:         types.ConnectorTypeNotion,
		Name:         "Notion",
		Description:  "Sync pages and databases from Notion",
		Priority:     1,
		AuthType:     "api_key",
		Capabilities: []string{"incremental"},
	},
	types.ConnectorTypeConfluence: {
		Type:         types.ConnectorTypeConfluence,
		Name:         "Confluence",
		Description:  "Sync spaces and pages from Atlassian Confluence",
		Priority:     2,
		AuthType:     "api_key",
		Capabilities: []string{"incremental"},
	},
	types.ConnectorTypeYuque: {
		Type:         types.ConnectorTypeYuque,
		Name:         "Yuque (语雀)",
		Description:  "Sync knowledge bases and documents from Yuque",
		Priority:     3,
		AuthType:     "api_key",
		Capabilities: []string{"incremental"},
	},
	types.ConnectorTypeGitHub: {
		Type:         types.ConnectorTypeGitHub,
		Name:         "GitHub",
		Description:  "Sync repositories, wikis, and issues from GitHub",
		Priority:     4,
		AuthType:     "oauth2",
		Capabilities: []string{"incremental"},
	},
	types.ConnectorTypeGoogleDrive: {
		Type:         types.ConnectorTypeGoogleDrive,
		Name:         "Google Drive",
		Description:  "Sync documents and files from Google Drive",
		Priority:     5,
		AuthType:     "oauth2",
		Capabilities: []string{"incremental"},
	},
	types.ConnectorTypeOneDrive: {
		Type:         types.ConnectorTypeOneDrive,
		Name:         "OneDrive / SharePoint",
		Description:  "Sync documents and files from Microsoft OneDrive",
		Priority:     6,
		AuthType:     "oauth2",
		Capabilities: []string{"incremental"},
	},
	types.ConnectorTypeDingTalk: {
		Type:         types.ConnectorTypeDingTalk,
		Name:         "DingTalk (钉钉)",
		Description:  "Sync documents and content from DingTalk",
		Priority:     7,
		AuthType:     "api_key",
		Capabilities: []string{"incremental"},
	},
	types.ConnectorTypeWebCrawler: {
		Type:         types.ConnectorTypeWebCrawler,
		Name:         "Web Crawler (Sitemap)",
		Description:  "Crawl websites via Sitemap.xml",
		Priority:     9,
		AuthType:     "none",
		Capabilities: []string{},
	},
	types.ConnectorTypeSlack: {
		Type:         types.ConnectorTypeSlack,
		Name:         "Slack",
		Description:  "Sync channel messages and files from Slack",
		Priority:     10,
		AuthType:     "oauth2",
		Capabilities: []string{"incremental"},
	},
	types.ConnectorTypeIMAP: {
		Type:         types.ConnectorTypeIMAP,
		Name:         "Email (IMAP)",
		Description:  "Sync email content from IMAP servers",
		Priority:     11,
		AuthType:     "password",
		Capabilities: []string{},
	},
	types.ConnectorTypeRSS: {
		Type:         types.ConnectorTypeRSS,
		Name:         "RSS / Atom Feed",
		Description:  "Sync articles from RSS/Atom feeds",
		Priority:     12,
		AuthType:     "custom",
		Capabilities: []string{"incremental"},
	},
}

// ListAvailableConnectors returns all available connector metadata
// sorted by priority
func ListAvailableConnectors() []ConnectorMetadata {
	metadata := make([]ConnectorMetadata, 0, len(ConnectorMetadataRegistry))
	for _, meta := range ConnectorMetadataRegistry {
		metadata = append(metadata, meta)
	}

	// Sort by priority (insertion sort for simplicity)
	for i := 1; i < len(metadata); i++ {
		key := metadata[i]
		j := i - 1
		for j >= 0 && metadata[j].Priority > key.Priority {
			metadata[j+1] = metadata[j]
			j--
		}
		metadata[j+1] = key
	}

	return metadata
}
