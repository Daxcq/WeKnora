package plugin

import (
	"strings"
	"testing"

	"github.com/Tencent/WeKnora/pkg/pluginapi"
)

func TestNewDockerCommandEnforcesManifestNetworkPolicy(t *testing.T) {
	cmd, _, err := newDockerCommand(pluginapi.Manifest{
		ID:          "network-probe",
		Permissions: pluginapi.Permissions{AllowNetwork: false},
		Runtime:     pluginapi.Runtime{Type: "docker", Image: "example/network-probe:dev"},
	}, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	args := strings.Join(cmd.Args, " ")
	if !strings.Contains(args, "--network none") {
		t.Fatalf("expected network isolation, got %s", args)
	}
	if !strings.Contains(args, "WEKNORA_PLUGIN_STDIO=1") {
		t.Fatalf("expected stdio transport, got %s", args)
	}
	if !strings.Contains(args, " run --rm -i ") {
		t.Fatalf("expected interactive stdin for gRPC transport, got %s", args)
	}
	if !strings.Contains(args, "--read-only") {
		t.Fatalf("expected read-only container filesystem, got %s", args)
	}
	if strings.Contains(args, " -p ") {
		t.Fatalf("network-isolated plugin must not publish a port: %s", args)
	}
}

func TestNewDockerCommandPreservesWindowsHostReadPath(t *testing.T) {
	cmd, _, err := newDockerCommand(pluginapi.Manifest{
		ID:          "local-files",
		Permissions: pluginapi.Permissions{ReadPaths: []string{"D:/notes"}},
		Runtime:     pluginapi.Runtime{Type: "docker", Image: "example/local-files:dev"},
	}, "/plugins/local-files")
	if err != nil {
		t.Fatal(err)
	}
	args := strings.Join(cmd.Args, " ")
	if !strings.Contains(args, "src=D:/notes") {
		t.Fatalf("expected Windows host path to remain absolute, got %s", args)
	}
}

func TestConfigMetadata(t *testing.T) {
	fields := configMetadata([]pluginapi.ConfigField{{
		Name: "settings.root_path", Type: "directory", Required: true,
	}})
	if len(fields) != 1 || fields[0].Name != "settings.root_path" || !fields[0].Required {
		t.Fatalf("unexpected metadata: %+v", fields)
	}
}
