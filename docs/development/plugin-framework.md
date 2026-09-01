# External Plugin Framework

## Scope

The first implementation externalizes data-source connectors. Document parsing,
embedding, retrieval engines, web search, and model providers continue to use
their existing in-process registries. They can reuse the same manifest and
lifecycle ideas later, but changing all four extension points at once would
make compatibility and rollback harder to verify.

## Chosen route

External plugins use a long-running process and gRPC. The wire payload is JSON
over gRPC so a plugin can be implemented in Go, Python, or another language
without importing `internal/...` packages. The public contract is in
`pkg/pluginapi`.

The host does the following:

1. Scans `WEKNORA_PLUGIN_DIR` (one child directory per plugin).
2. Reads `manifest.json`, `manifest.yaml`, or `manifest.yml`.
3. Checks the protocol and WeKnora version range.
4. Starts the declared runtime and checks `GetManifest` and `Health`.
5. Wraps the gRPC client as the existing `datasource.Connector` interface.
6. Stops the process during application cleanup.

Built-in and external data-source connectors are both registered in
`ConnectorRegistry`. `SetEnabled` controls whether new requests can resolve a
connector, and `Health` checks the same registration; external connectors
delegate that check to the gRPC health RPC. Administrators can inspect the
runtime snapshot at `GET /api/v1/datasource/types/status` and change the
in-memory enabled state with `PUT /api/v1/datasource/types/{type}` and a body
such as `{"enabled":false}`.

The existing `DataSourceService` therefore keeps the same full/incremental
sync, cursor, ingestion, and logging behavior for built-in and external
connectors.

## Manifest

```json
{
  "id": "local-files",
  "name": "Local Files",
  "version": "0.1.0",
  "protocol_version": 1,
  "extension_types": ["datasource"],
  "weknora_version": ">=0.6.0 <1.0.0",
  "config": [
    {"name": "settings.root_path", "type": "directory", "required": true}
  ],
  "permissions": {
    "allow_network": false,
    "read_paths": ["D:/data/notes"]
  },
  "runtime": {
    "type": "docker",
    "image": "example/local-files:0.1.0",
    "port": 50051
  }
}
```

`allow_network: false` is a security requirement, not just metadata. A normal
host process cannot truthfully promise that it cannot create an outbound
socket, so the loader rejects a process runtime with network disabled. Docker
plugins use `--network none` and carry gRPC over the plugin process's stdin and
stdout, so the control channel does not require a network interface. Plugin
stderr is captured by the host and logged; a connector should report an
attempted external request as an error rather than hiding it.

`read_paths` is the host-side allowlist mounted read-only at
`/weknora/inputs/<index>` in the container. The connector configuration should
use the container path, such as `/weknora/inputs/0`.

Docker-runtime plugins require the app container to have the Docker CLI and a
bind mount of `/var/run/docker.sock`. The app uses that socket only to start
and stop the declared plugin image; the plugin container does not receive the
socket. The socket API remains privileged even if the bind mount is marked
read-only. Access to the Docker socket is equivalent to access to the host
Docker daemon, so enable this mode only for a trusted WeKnora deployment.
On Docker Desktop, `permissions.read_paths` must use a host path shared with
Docker, such as `D:/data/notes`; the same path is passed to the Docker daemon
for its read-only bind mount.

## RPCs

The plugin implements:

```text
GetManifest
Health
Validate
ListResources
ResolveResourceAncestors
FetchAll
FetchIncremental
Shutdown
```

The plugin returns source data only. It does not parse documents, chunk text,
call embedding models, or write to the knowledge base.

## Example

`examples/plugins/local-files` is a standalone module. It uses the published
WeKnora `v0.6.5` SDK through the current Daxcq fork replacement. It uses a
relative `ExternalID`, SHA-256 content hashes, and a cursor containing the
current file map. A rename is therefore represented as a deletion plus an
addition. Build its image from the plugin directory and copy the manifest into
the configured plugin directory:

```powershell
docker build -t weknora-plugin-local-files:dev examples/plugins/local-files
$env:WEKNORA_PLUGIN_DIR = "D:\weknora-plugins"
```

For the standard Docker Compose deployment, set
`WEKNORA_PLUGIN_HOST_DIR` to a host directory containing one child directory
per plugin. Compose mounts it at `/plugins` and sets `WEKNORA_PLUGIN_DIR` for
the app automatically. The Compose app service also mounts the Docker socket
and includes the Docker CLI for Docker-runtime plugins. A `process` runtime plugin must include its executable
in that mounted directory and use a command path inside the container, such
as `/plugins/local-files/local-files`. This runtime does not provide network or
filesystem isolation; use the Docker runtime for plugins that declare
`allow_network: false`.

Before running, set `permissions.read_paths` in its manifest to the host
directory that should be mounted, then configure the data source's
`settings.root_path` as `/weknora/inputs/0`.

## Create a plugin

Copy `examples/plugins/template` into a separate repository. Replace
`replace-me` in `main.go` and `manifest.json`, then implement the five
`pluginapi.Connector` methods. The template already depends on the released
WeKnora module version and contains no path back to this checkout.

Build and test from the plugin repository:

```powershell
go test ./...
docker build -t replace-me:dev .
```

Copy only the manifest into a child directory of `WEKNORA_PLUGIN_DIR` when the
runtime image is already available on the host. Restart WeKnora and confirm the
startup log contains `external datasource plugin loaded`.

## Verification

The local-files module has an automated content-hash incremental test:

```powershell
cd examples/plugins/local-files
go test .
```

The test creates `a.md` and `c.md`, changes only `c.md`, and verifies that the
incremental response contains only `c.md`. A separate test covers
`IsDeleted=true` tombstones.

To verify network enforcement, build `examples/plugins/network-probe`, install
its manifest, then trigger one data-source sync. The plugin deliberately calls
`https://example.com`. Because its manifest declares `allow_network: false`,
the host starts it with Docker `--network none`; the sync fails and both the
blocked request and plugin ID appear in the WeKnora log. The host-side command
test also asserts that no port is published:

```powershell
docker build -t weknora-plugin-network-probe:dev examples/plugins/network-probe
docker run --rm --network none -e WEKNORA_NETWORK_PROBE_SELF_TEST=1 weknora-plugin-network-probe:dev
go test ./internal/datasource/plugin -run TestNewDockerCommandEnforcesManifestNetworkPolicy
$env:WEKNORA_TEST_PLUGIN_IMAGE = "weknora-plugin-local-files:dev"
go test ./pkg/pluginapi -run TestDockerStdioRoundTrip
```

## Failure and deletion rules

An external process crash fails the sync and leaves the previous knowledge and
cursor intact. A connector reports a source deletion with `IsDeleted=true`.
`sync_deletions=false` ignores that event. When it is enabled, the
`deletion_policy` field controls the host-side action:

- `retain` (default): keep the knowledge and finish the sync without deleting
  derived data.
- `delete`: call WeKnora's normal knowledge deletion flow, which removes
  chunks, vectors, files, graph data, and soft-deletes the knowledge row.

The connector only reports the change; it never deletes host data itself.
