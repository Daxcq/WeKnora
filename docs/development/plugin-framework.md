# External Plugin Framework

## Scope

The current implementation externalizes data-source connectors, document
parser engines, web-search providers, and chat model providers. Retrieval
engines and the other model capabilities still use their existing in-process
registries. External model providers currently implement the `chat` contract;
Embedding, Rerank, VLM, and ASR are intentionally separate follow-up
contracts.

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
5. Wraps the gRPC client as the existing datasource connector, document
   reader, web-search provider, or chat model interface.
6. Stops the process during application cleanup.

Built-in and external data-source connectors are both registered in
`ConnectorRegistry`. `SetEnabled` controls whether new requests can resolve a
connector, and `Health` checks the same registration; external connectors
delegate that check to the gRPC health RPC. Administrators can inspect the
runtime snapshot at `GET /api/v1/datasource/types/status` and change the
enabled state with `PUT /api/v1/datasource/types/{type}` and a body such as
`{"enabled":false}`. Disabled connector IDs are persisted in the
`plugins.disabled` system setting and restored on restart.

The existing `DataSourceService` therefore keeps the same full/incremental
sync, cursor, ingestion, and logging behavior for built-in and external
connectors. Parser plugins return Markdown; the existing knowledge pipeline
continues with status tracking, chunking, embedding, and indexing. Web-search
plugins return normalized results; filtering and RAG compression remain in the
host service.

## Manifest

```json
{
  "id": "local-files",
  "name": "Local Files",
  "version": "0.1.0",
  "protocol_version": 1,
  "extension_types": ["datasource"],
  "file_types": ["md", "markdown"],
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
Parse (when `extension_types` contains `document_parser`)
Search (when `extension_types` contains `web_search`)
Chat (when `extension_types` contains `model_provider` and `model_types` contains `chat`)
ValidateModelConfig (for external model configuration)
Shutdown
```

Datasource plugins return source data only. Parser plugins return parsed
Markdown only. Web-search plugins return search results only. None of them
chunks text, calls embedding models, or writes to the knowledge base.

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

The separate checkout at `D:\weknora-plugin-local-files` follows the same
layout and does not import any `internal/...` package from this repository:

```powershell
cd D:\weknora-plugin-local-files
go test ./...
docker build -t weknora-plugin-local-files:dev .
```

Install it by placing its `manifest.json` in a child directory of the host
plugin directory, then restart WeKnora. The data-source picker should expose
`Local Files`, and `设置 → 数据与扩展 → 插件管理` should show it as healthy.
The image must exist on the same Docker host because WeKnora starts it through
the Docker socket.

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

For a minimal parser, copy `examples/plugins/plain-text-parser`. Its `Parse`
method handles Markdown and text files. Set `file_types` in the manifest and
select the plugin ID in a parser-engine rule.

Minimal chat model manifest fields:

```json
{
  "id": "my-chat-provider",
  "name": "My Chat Provider",
  "version": "0.1.0",
  "protocol_version": 1,
  "extension_types": ["model_provider"],
  "model_types": ["chat"],
  "weknora_version": ">=0.6.6 <1.0.0",
  "permissions": {"allow_network": true},
  "runtime": {"type": "docker", "image": "example/my-chat-provider:0.1.0"}
}
```

The plugin implements `ValidateConfig(ctx, pluginapi.ModelConfig)` and
`Chat(ctx, *pluginapi.ModelChatRequest)`. `ModelConfig` contains the selected
model name, base URL, API key, extra configuration, and custom headers. The
plugin may use any upstream model protocol internally.

A web-search plugin declares `"extension_types": ["web_search"]` and
implements `pluginapi.WebSearchProvider`. Its `Search` method receives the
plugin-owned JSON configuration, query, result limit, and date option. Once
loaded, it appears in the same provider picker as built-in search providers.

A chat model plugin declares `"extension_types": ["model_provider"]` and
`"model_types": ["chat"]`, then implements `pluginapi.ModelProvider`. The
host sends the model configuration, messages, tools, and options over gRPC.
The plugin returns one normalized response; the host exposes it through the
normal WeKnora chat and stream interfaces. This first contract is unary at the
plugin boundary, so it does not provide token-by-token streaming from the
plugin.

Copy only the manifest into a child directory of `WEKNORA_PLUGIN_DIR` when the
runtime image is already available on the host. Restart WeKnora and confirm the
startup log contains `external datasource plugin loaded`. The settings page
manages loaded runtime state; installation, removal, and image updates remain
directory/Docker operations.

## Verification

The local-files module has an automated content-hash incremental test:

```powershell
cd examples/plugins/local-files
go test .
```

The test creates `a.md` and `c.md`, changes only `c.md`, and verifies that the
incremental response contains only `c.md`. A separate test covers
`IsDeleted=true` tombstones.

For a UI end-to-end check, put `a.md` and `c.md` in the allowed host directory,
run one full sync, change only `c.md`, and run an incremental sync. The app
log should contain `incremental sync fetched 1 items` and the sync result
should show one updated item. Removing `a.md` should produce a deletion
tombstone; the host applies it according to `sync_deletions` and
`deletion_policy`.

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
