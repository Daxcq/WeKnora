# External plugin examples

These directories are standalone Go modules. They import only the public
`github.com/Tencent/WeKnora/pkg/pluginapi` package from WeKnora `v0.6.4` and do
not import `internal/...` packages.

Copy any example directory into a separate Git repository and run:

```powershell
go test ./...
docker build -t weknora-plugin-local-files:dev .
```

Install the manifest as one child directory of `WEKNORA_PLUGIN_DIR`. For a
Docker runtime, the image must already exist on the Docker host. For a process
runtime, the executable must be present in the mounted plugin directory.

The SDK is published as `v0.6.4`, so these examples can be built from an
empty external repository without a local `replace` directive.
