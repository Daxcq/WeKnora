package datasource

import (
	"context"
	"errors"
	"testing"

	"github.com/Tencent/WeKnora/internal/types"
)

func TestFeishuMetadataDoesNotAdvertiseWebhook(t *testing.T) {
	meta := ConnectorMetadataRegistry[types.ConnectorTypeFeishu]

	for _, capability := range meta.Capabilities {
		if capability == "webhook" {
			t.Fatalf("Feishu connector should not advertise webhook until webhook sync is implemented")
		}
	}
}

type registryTestConnector struct{ connectorType string }

func (c registryTestConnector) Type() string { return c.connectorType }
func (registryTestConnector) Validate(context.Context, *types.DataSourceConfig) error {
	return nil
}
func (registryTestConnector) ListResources(context.Context, *types.DataSourceConfig, string) ([]types.Resource, error) {
	return nil, nil
}
func (registryTestConnector) ResolveResourceAncestors(context.Context, *types.DataSourceConfig, []string) ([]string, error) {
	return nil, nil
}
func (registryTestConnector) FetchAll(context.Context, *types.DataSourceConfig, []string) ([]types.FetchedItem, error) {
	return nil, nil
}
func (registryTestConnector) FetchIncremental(context.Context, *types.DataSourceConfig, *types.SyncCursor) ([]types.FetchedItem, *types.SyncCursor, error) {
	return nil, nil, nil
}

func TestConnectorRegistryRejectsDuplicateTypes(t *testing.T) {
	r := NewConnectorRegistry()
	if err := r.Register(registryTestConnector{connectorType: "test"}); err != nil {
		t.Fatal(err)
	}
	if err := r.Register(registryTestConnector{connectorType: "test"}); !errors.Is(err, ErrConnectorDuplicate) {
		t.Fatalf("expected duplicate error, got %v", err)
	}
}

func TestConnectorRegistryEnableDisableAndHealth(t *testing.T) {
	r := NewConnectorRegistry()
	if err := r.Register(registryTestConnector{connectorType: "test"}); err != nil {
		t.Fatal(err)
	}
	if err := r.Health(context.Background(), "test"); err != nil {
		t.Fatal(err)
	}
	if err := r.SetEnabled("test", false); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Get("test"); !errors.Is(err, ErrConnectorDisabled) {
		t.Fatalf("expected disabled error, got %v", err)
	}
	if len(r.List()) != 0 {
		t.Fatal("disabled connector must not be listed as available")
	}
	if err := r.SetEnabled("test", true); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Get("test"); err != nil {
		t.Fatal(err)
	}
}

func TestConnectorRegistryStatusesAreSortedAndIncludeDisabledHealth(t *testing.T) {
	r := NewConnectorRegistry()
	if err := r.Register(registryTestConnector{connectorType: "zeta"}); err != nil {
		t.Fatal(err)
	}
	if err := r.Register(registryTestConnector{connectorType: "alpha"}); err != nil {
		t.Fatal(err)
	}
	if err := r.SetEnabled("zeta", false); err != nil {
		t.Fatal(err)
	}

	statuses := r.Statuses(context.Background())
	if len(statuses) != 2 || statuses[0].Type != "alpha" || statuses[1].Type != "zeta" {
		t.Fatalf("unexpected statuses: %#v", statuses)
	}
	if statuses[0].Enabled != true || statuses[1].Enabled != false {
		t.Fatalf("unexpected enabled states: %#v", statuses)
	}
}
