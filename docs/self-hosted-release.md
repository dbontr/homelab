# Self-hosted release

The repository is the source of truth for the Go platform, independent Engineering execution, Compute, Learning, Health and the Rust/Dioxus console. These are capabilities of one product, with internal process isolation for credentials and failure recovery. The release process is independent of GitHub Actions and never grants deployment credentials to the primary MCP gateway.

## Release staging

Use an operator-controlled host with the Go toolchains required by the four Go modules, Rust, Cargo, the WebAssembly target and Dioxus CLI available. The output directory must be new, and its parent must already exist.

`bash scripts/stage-release.sh /srv/releases/homelab/<unique-release-id>`

The staging script requires a clean, exact 40-character Git source revision and an operator-owned, non-symlink release parent that is not group or world writable. Before building, it runs `scripts/verify-offline.sh --all`, including module tests, Rust checks, Helm rendering, fleet NixOS evaluations and local package builds. It builds eleven Go executables from cached source dependencies, packages the Dioxus web assets, and writes `SHA256SUMS` and `RELEASE`. The independent `scripts/verify-release.sh` then checks the release envelope, declared binary set, exact source revision, and digest of every asset before the temporary directory is atomically published. It does not alter NixOS generations, start services, publish images, stop the existing homepage, or touch application databases.

Before publication, staging gives binary files read/execute permission and static files read permission for isolated service accounts, while removing write permission from the release tree. The release parent and all its ancestors must permit traversal by the intended service users and remain under operator-controlled write custody.

A release consumer can inspect an artifact without deploying it using `bash scripts/verify-release.sh /srv/releases/homelab/<release-id>`. The checksum manifest detects corruption and undeclared files; it does not authenticate a malicious replacement of the entire manifest and payload. Trust the release only when its staging directory, original source commit, and custody are controlled by the independent operator. The verifier never grants activation authority or changes a running generation.

## Source-owned Learning and Health binaries

The Learning and Health service modules can consume binaries from the unified local source release without changing the application's persisted databases or authentication configuration. For a verified release at `/srv/releases/homelab/<release-id>`, configure `homelab.learning.binaryPath` as `/srv/releases/homelab/<release-id>/bin/homelab-learning` and `homelab.personalHealth.binaryPath` as `/srv/releases/homelab/<release-id>/bin/homelab-health`. Supply each file's exact 64-character SHA-256 in its matching `binarySHA256` Nix option. The unit-level prestart checks retain the hash pin. The legacy standalone Health release path remains supported for a separately approved transition.

These options specify future host configuration only. They do not copy files, switch active generations, restart either service, or migrate private state. Before activation, the operator must verify the whole bundle with `scripts/verify-release.sh`, independently verify the manifest and source commit custody, confirm service-account traversal rights for the full release path, and preserve existing state directories and credential files. A release built from local source is not accepted until its service API, database compatibility and health checks pass on the target host.

Compute toolchain environments can be built locally with:

`bash system/compute/scripts/build-toolchain-image.sh <approved-image-reference> --load`

Use `--push` only when an operator has configured an approved registry, narrow package-write credential and verified image digest. Rootless BuildKit Kubernetes jobs are separately opt-in in the compute implementation. Publishing an image does not enroll it into a worker or activate a Kubernetes release.

## Coordinated host releases

The `release.FleetCoordinator` orders declared host operations by explicit release ID. It activates compute hosts serially and permits at most one control-plane host, in the final position. On failure it requests reversal of previously completed host generations in reverse order. A failed host is marked as needing inspection because its state may be unknown. An incomplete rollback is reported rather than concealed. Each host's independent release service must perform actual authorization, health checks and durable journaling.

This coordinator is a source-level component; it requires authenticated per-host transport, persistent fleet journal and staged host artifacts before activation is enabled in production. It does not replace the local NixOS generation controller.

The local CLI also supports `sudo homelab-releasectl revert STAGED_RELEASE_ID` for a previously activated generation. Reversion checks the exact active release journal and deployed Nix store path. Replay and out-of-band generation drift are rejected; interrupted reversion requires recovery.

## Guarded NixOS generation controller

The local `homelab-releasectl` wrapper is installed only when `services.homelabExecutor.enable` is configured. It executes as a trusted local administrator and uses the same private release journal as the independent executor:

```sh
sudo homelab-releasectl stage EXACT_40_CHARACTER_COMMIT
sudo homelab-releasectl status
sudo homelab-releasectl activate STAGED_RELEASE_ID
sudo homelab-releasectl recover
```

The staging command validates the exact checked-out Git commit and requires a clean working tree. A remote stage request must be approved by the independent owner authorizer and must append a durable, hashed `started` audit entry before invoking any Nix build. A corresponding `succeeded` or `failed` entry records the result. Missing audit custody denies staging, and a failed completion append leaves the operation's outcome explicitly uncertain. The audit journal does not contain source revisions, credentials, or raw request arguments. It uses `nix build --offline --no-link --print-out-paths` to obtain the local host's `config.system.build.toplevel` store path. No arbitrary path can be selected by an MCP call. All generation records are kept in a private, process-locked directory with fsynced atomic updates.

Activation records the previous NixOS system before changing the profile. It switches to the staged generation, probes exact allowlisted systemd services, and restores the previous generation when the switch or a health check fails. A crash during activation leaves an interrupted journal that must be handled by `recover` before another activation. The controller is local-host scoped; independent staging and acceptance are required for each compute node.

The executor permits only exact local service restart names and staging IDs. Its remote activation operation is separately disabled unless the operator enables `services.homelabExecutor.allowReleaseActivation`. This option, the signed owner identity, and a destructive-operation confirmation are all required before a ChatGPT-triggered activation is considered. Routing, firewall, media and Kubernetes-critical services are excluded from automated restart allowlists. Network changes and Saturn routing releases require a tested out-of-band recovery plan.

## Activation

The Go platform and Rust console have separate NixOS activation switches. They remain disabled unless an operator enables them and supplies the required protected credentials. The existing Homepage dashboard stays active until a release is accepted and the console's explicit `replaceHomepage` option is enabled.

The internal compute, learning and health services retain their own state and release ownership. The imported source in this repository does not automatically replace their active binaries or modify Kubernetes controllers. An operator must select locally built, digest-qualified artifacts in the relevant service definitions and provide a rollback plan for each replacement.

## Acceptance before activation

1. Confirm the staged binary and asset SHA-256 manifest and the corresponding source revision.
2. Validate the Go modules, Rust/WASM build, NixOS host evaluations and service package derivations against the selected revision.
3. Confirm the owner browser session, MCP authentication, backend discovery, private monitoring access, calendar read-only behavior and blocked unauthorized actions.
4. Confirm worker image admission, registry push/pull, state retention, hardware identity and Kubernetes Job lifecycle independently of the public gateway.
5. Inspect existing media, DNS, routing and Cloudflare paths, and preserve access to Moonfin and RecipeSage before any cutover.
6. Activate one service at a time, verify readiness and retain the previous NixOS generation and immutable release. Avoid changing home routing until every dependent service is accepted.

Source staging is not evidence of these acceptance checks. Do not delete databases, previous application deployments, existing repositories, recovery points or media merely because a newer source build exists.

## Canonical resource references

The platform uses references of the form `homelab:<kind>:<id>`, where kind is `task`, `goal`, `recipe`, `meal`, or `book`. `resource_resolve` and the native `GET /v1/resources/resolve?ref=...` route resolve a reference through the owning first-party service rather than copying application records into a shadow store. Invalid or unsupported identities fail closed. Compute project and research-library references remain owned by their service until an authenticated cross-service resolver is implemented.

## Local monitoring

Monitoring reuses the private Prometheus and Loki services. The platform's monitoring adapter accepts only loopback origins, fixed PromQL expressions and bounded Loki journal queries. The Rust dashboard can replace the Grafana presentation after compilation and acceptance. The collectors, retention rules and alert evaluation remain independent of the frontend.

## Engineering-run boundary

The independent `homelab-engineeringd` package and NixOS module are staged as part of the unified release. The daemon is opt-in. It requires protected project-scoped compute credentials and a finite list of allowed projects. An optional source model has separate credentials. The gateway receives neither secret; it can submit, inspect or cancel an engineering run only through the local Unix lifecycle socket.

An engineering run may execute isolated source tasks and request a revision-guarded draft pull request. It cannot merge, change the NixOS fleet, restart critical services, or bypass the independent privileged executor. See [Engineering execution](engineering.md).
