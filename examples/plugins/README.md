# External plugin examples

These directories are standalone Go modules. They import only the public
`github.com/Tencent/WeKnora/pkg/pluginapi` package from WeKnora `v0.6.5` and do
not import `internal/...` packages.

Copy any example directory into a separate Git repository and run:

```powershell
go test ./...
docker build -t weknora-plugin-local-files:dev .
```

`local-files` is a datasource example. `plain-text-parser` is the smallest
document-parser example; its `Parse` method returns Markdown and the host
continues with chunking and embedding.

Install the manifest as one child directory of `WEKNORA_PLUGIN_DIR`. For a
Docker runtime, the image must already exist on the Docker host. For a process
runtime, the executable must be present in the mounted plugin directory.

For Docker-runtime plugins, the WeKnora app deployment must mount the Docker
socket and install the Docker CLI. The standard `docker-compose.yml` does
this. This grants the app access to the host Docker daemon, so only install
trusted plugin manifests and images.

The examples use the `Daxcq/WeKnora` fork as the published source for now:
the `replace` directive maps the canonical Tencent module path to the fork's
`v0.6.5` tag. Once the SDK is published to `Tencent/WeKnora`, remove that one
`replace` line; the plugin source itself does not change.
