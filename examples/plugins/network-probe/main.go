package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net"
	"os"
	"time"

	"github.com/Tencent/WeKnora/pkg/pluginapi"
)

type networkProbe struct{}

var errNetworkAllowed = errors.New("network unexpectedly allowed")

func (networkProbe) Validate(context.Context, json.RawMessage) error { return nil }

func (networkProbe) ListResources(context.Context, json.RawMessage, string) ([]pluginapi.Resource, error) {
	return nil, nil
}

func (networkProbe) ResolveResourceAncestors(context.Context, json.RawMessage, []string) ([]string, error) {
	return nil, nil
}

func (networkProbe) FetchAll(ctx context.Context, _ json.RawMessage, _ []string) ([]pluginapi.FetchedItem, error) {
	dialer := net.Dialer{Timeout: 3 * time.Second}
	conn, err := dialer.DialContext(ctx, "tcp", "example.com:443")
	if err == nil {
		_ = conn.Close()
		return nil, errNetworkAllowed
	}
	log.Printf("outbound TCP connection blocked as expected: %v", err)
	return nil, fmt.Errorf("network blocked: %w", err)
}

func (networkProbe) FetchIncremental(ctx context.Context, raw json.RawMessage, cursor json.RawMessage) ([]pluginapi.FetchedItem, json.RawMessage, error) {
	items, err := networkProbe{}.FetchAll(ctx, raw, nil)
	return items, cursor, err
}

func main() {
	if os.Getenv("WEKNORA_NETWORK_PROBE_SELF_TEST") == "1" {
		_, err := networkProbe{}.FetchAll(context.Background(), nil, nil)
		if err == nil || errors.Is(err, errNetworkAllowed) {
			log.Fatal(errNetworkAllowed)
		}
		log.Print("network isolation verified")
		return
	}
	manifest := pluginapi.Manifest{
		ID: "network-probe", Name: "Network Probe", Version: "0.1.0",
		ProtocolVersion: pluginapi.ProtocolVersion, ExtensionTypes: []string{"datasource"},
		WeKnoraVersion: ">=0.6.0 <1.0.0",
		Permissions:    pluginapi.Permissions{AllowNetwork: false},
		Runtime:        pluginapi.Runtime{Type: "docker", Image: "weknora-plugin-network-probe:dev"},
		Description:    "Fails a sync after proving outbound traffic is blocked.",
	}
	if err := pluginapi.Serve(manifest, networkProbe{}); err != nil {
		log.Fatal(err)
	}
}
