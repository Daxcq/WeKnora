# Security Scan Triage (Rhino-bird 2026, Topic 1)

This note records the triage of the Mimosa deep static security scan run
against the plugin-framework branch, plus an independent dependency advisory
check. It is part of the topic submission evidence.

- Scan: `scan-2026-09-10T15-49-06.828Z-de6a425e2bb6`
- Seal: `sha256:b5895dbd434accfc7d49b6b67991d8f57462b74aade6d6d6d68c6daa50405eff`
- Depth: deep, 1524 files parsed, 73 findings, 0 business-logic candidates.
- Run status: **inconclusive by design of the tool** — call-graph coverage is
  partial for dynamic dispatch, so results are static advisories, not runtime
  proof. No claim of "fully secure" is made anywhere in this document.

## 1. Findings touching this submission's code

### 1.1 Command injection at `internal/datasource/plugin/manager.go:372` — accepted risk, by design

The flagged line executes `manifest.Runtime.Command` with `manifest.Runtime.Args`.
Both values come only from the plugin manifest that the operator places inside
`WEKNORA_PLUGIN_DIR` on the host. There is no remote or request-driven input on
this path: the HTTP API never accepts command or argument fields, and the
datasource connector config is passed over gRPC, not to `exec.Command`.
Loading a manifest is equivalent to installing a binary and is the documented
trust boundary of the framework ("only install trusted plugin manifests and
images", see `plugin-framework.md`). The Docker runtime additionally validates
image names and never forwards socket access into the plugin container.
Enforcement is covered by unit tests such as
`TestNewDockerCommandEnforcesManifestNetworkPolicy`.

### 1.2 Tainted file path at `examples/plugins/local-files/main.go:80` — matches design intent

The path is read from `settings.root_path`, which is constrained host-side: for
the Docker runtime only directories listed in the manifest `permissions.read_paths`
are bind-mounted read-only at `/weknora/inputs/<N>`. A plugin cannot reach a host
path outside that allowlist regardless of what the config value says; within the
allowlist, reading files is the connector's job.

### 1.3 "Hardcoded credential" findings in `frontend/src/i18n/locales/*.ts` — false positives

All 40+ matches are i18n message keys whose names contain `apiKey`,
`password`, or `secret` (e.g. `enterApiKey`, `playgroundNeedApiKey`,
`authTypeApiKey`, `webhookSecret`). They are UI translation labels, not
credential values. Verified line by line.

### 1.4 Findings in pre-existing upstream code

The remaining HIGH advisories (desktop updater and sandbox `exec` sites,
`docreader/` path traversal and SSRF, weak-hash usages, cross-file taint
hints) sit in code that already exists on upstream `Tencent/WeKnora` `main`
and was not introduced or modified by this submission. They are listed in the
sealed artifacts and are worth a maintainer-side pass in a separate effort.

## 2. Dependency advisories

The scanner's offline snapshot matched 11 packages / 59 advisories. An
independent OSV.dev query of the full Go module graph (1125 versioned modules,
the same list the scanner summarizes as ~808 scanned packages) reports 168
advisories over 37 module entries; the difference is snapshot staleness, and
the OSV list is used as the reference below.

Key fact for this submission: **the plugin framework introduces zero new
dependencies.** `git diff $(git merge-base origin/main rhino-2026-final-1)..rhino-2026-final-1 -- go.mod`
is empty. Every affected package is inherited from upstream WeKnora or is a
transitive graph entry not imported by any built binary.

Direct requires with advisories (all pre-existing upstream):
`golang.org/x/crypto`, `golang.org/x/net`, `golang.org/x/mod`,
`google.golang.org/grpc`, `github.com/ollama/ollama`, `github.com/weaviate/weaviate`,
`github.com/xuri/excelize/v2`.

gRPC (v1.81.0, used by the plugin transport) has 5 advisories; 4 of the 5 are
xDS-only (`xDS servers`, `xDS RBAC`) and WeKnora does not enable xDS. The
fifth, GHSA-vp52-pcj8-j9qc (HTTP/2 DATA fragmentation OOM), applies to any
gRPC-Go server, but both plugin transports are loopback or stdio-only: the
process runtime binds `127.0.0.1` and the Docker runtime carries gRPC over
stdin/stdout, so no remote client can reach the server. Bumping
`google.golang.org/grpc` to the fixed line is still the right maintainer-side
housekeeping action and is recommended to upstream.

Same reasoning applies to indirect graph entries such as `go-git`, `fiber`,
`nats-io/jwt`, `docker/docker`: they are not shipped code paths of this
submission, and the local plugin gRPC path never uses `net.Listen("tcp", ...)`
on a routable interface.

## 3. Reproducing this triage

- Query the dependency list: `go list -m all` piped into the OSV
  `querybatch` API (the batch used in §2 was generated exactly this way).
- The sealed scan artifacts (`findings.json`, `coverage.json`, `seal.json`)
  live outside the repository under the local Mimosa scan history directory;
  the scan ID and seal above identify them unambiguously for verification.
