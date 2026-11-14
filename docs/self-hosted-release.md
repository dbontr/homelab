# Self-hosted release

The repository is the source of truth for the domain Go services, independent Engineering execution, Compute, Learning, Health, Rust/Dioxus UI, and optional MCP and API entry points. These are capabilities of one product, with internal process isolation for credentials and failure recovery. The release process is independent of GitHub Actions and never grants deployment credentials to the primary MCP gateway.

## Release staging

Use an operator-controlled host with the Go toolchains required by the four Go modules, Rust, Cargo, the WebAssembly target and Dioxus CLI available. The output directory must be new, and its parent must already exist.

`bash scripts/stage-release.sh /srv/releases/homelab/<unique-release-id>`

To stage one component without rebuilding unrelated services, use
`bash scripts/stage-release.sh --component homelab-api /srv/releases/homelab/<unique-component-release-id>`.
The accepted component names are the fourteen Go executable names in the full release, plus `homelab-console`. Each component release contains one executable or the console assets, with the same exact-source provenance, checksum verification, non-writable artifact rules and no activation. Component staging runs Go tests and vet for its owning module, or offline Rust/WASM checks for the console. It does not claim whole-fleet compatibility or authorize a deployment.

The trusted release CLI requires exclusive release-state ownership for staging, activation, reversion and recovery, so it cannot mutate release state while the privileged executor is running. Read-only status inspection remains available. Manual recovery requires an out-of-band administrator to stop the executor first.

The complete release includes separately executable `homelab-mcp` and `homelab-api` adapters. Building them does not enable their optional NixOS listeners or alter running services. The web UI is built from `ui/console` and requires neither external listener.

The staging script requires a clean, exact 40-character Git source revision and an operator-owned, non-symlink release parent that is not group or world writable. Before building, it runs `scripts/verify-offline.sh --all`, including module tests, Rust checks, Helm rendering, fleet NixOS evaluations and local package builds. It builds thirteen Go executables from cached source dependencies, packages the Dioxus web assets, and writes `SHA256SUMS` and `RELEASE`. The independent `scripts/verify-release.sh` then checks the release envelope, declared binary set, exact source revision, and digest of every asset before the temporary directory is atomically published. It does not alter NixOS generations, start services, publish images, stop the existing homepage, or touch application databases.

Before publication, staging gives binary files read/execute permission and static files read permission for isolated service accounts, while removing write permission from the release tree. The release parent and all its ancestors must permit traversal by the intended service users and remain under operator-controlled write custody.

A release consumer can inspect an artifact without deploying it using `bash scripts/verify-release.sh /srv/releases/homelab/<release-id>`. The checksum manifest detects corruption and undeclared files; it does not authenticate a malicious replacement of the entire manifest and payload. The staging-control tests use a disposable mock compiler and Git command to verify packaging, provenance validation, rejection and cleanup; they do not replace Go compilation or NixOS evaluation. Trust the release only when its staging directory, original source commit, and custody are controlled by the independent operator. The verifier never grants activation authority or changes a running generation.

## Source-owned Learning and Health binaries

The Learning and Health service modules can consume binaries from the unified local source release without changing the application's persisted databases or authentication configuration. For a verified release at `/srv/releases/homelab/<release-id>`, configure `homelab.learning.binaryPath` as `/srv/releases/homelab/<release-id>/bin/homelab-learning` and `homelab.personalHealth.binaryPath` as `/srv/releases/homelab/<release-id>/bin/homelab-health`. Supply each file's exact 64-character SHA-256 in its matching `binarySHA256` Nix option. The unit-level prestart checks retain the hash pin. The legacy standalone Health release path remains supported for a separately approved transition.

These options specify future host configuration only. They do not copy files, switch active generations, restart either service, or migrate private state. Before activation, the operator must verify the whole bundle with `scripts/verify-release.sh`, independently verify the manifest and source commit custody, confirm service-account traversal rights for the full release path, and preserve existing state directories and credential files. A release built from local source is not accepted until its service API, database compatibility and health checks pass on the target host.

## Source-owned Compute OCI releases

The Compute controller and Kubernetes workers are compiled from `services/compute` in this repository. The release operator can build both images from one clean, exact Git commit using the already-present local Dockerfiles:

```sh
bash scripts/build-compute-images.sh ghcr.io/operator/eminence /srv/releases/compute-images/<unique-id> --load
```

The default `--load` mode installs source-built images into the operator's local container runtime. It does not provide registry-pinned deployment references. Only the explicit `--push` mode publishes images using preconfigured, narrowly scoped registry credentials and records both registry-confirmed `@sha256:` identities in `IMAGE-RELEASE.json`; if either image fails, the deployable receipt is not published. The receipt does not contain credentials or Buildx metadata.

The local Compute Helm chart disables both components by default and refuses to render an enabled controller or worker with an empty or mutable image reference. After independently verifying the source revision and registry artifact custody, configure `controller.enabled` / `worker.enabled` and their exact digest-qualified `image` fields, or set the matching `services.workspaceMcp.controllerImage` and `workerImage` NixOS options. Registry publish does not imply Kubernetes deployment, worker enrollment or source-repository retirement. The current Dockerfiles rely on external base images and package repositories; independent base-image and package provenance must be accepted before claiming fully reproducible builds. The existing controller database volume, paired worker credentials, namespaces and jobs must survive the separately approved live cutover.

### Per-node registry pull acceptance

Use an operator-controlled Kubernetes context and independently verified, digest-qualified image references to test image retrieval without changing running Deployments:

```sh
IMAGE_PULL_NAMESPACE=pwm-system bash services/compute/scripts/verify-cluster-image-pulls.sh \
  "$CONTROLLER_IMAGE_DIGEST" "$WORKER_IMAGE_DIGEST" jupiter venus mercury janus
```

The validator first requires Ready nodes. For each node, it creates a restricted, non-root, single-use Pod for each role, with `imagePullPolicy: Always`, no service-account token, no privilege escalation, a read-only root filesystem, and bounded resource use. It verifies the completed container's image digest and exit code, an execution marker, and a Kubernetes `Pulled` event. JSON-lines receipts are printed to standard output; label-scoped Pods are removed on success or failure. Capture the receipts in an operator-controlled directory when release evidence must be retained. Registry and node errors fail acceptance.

A successful `Pulled` event verifies that kubelet retrieved or resolved the exact image reference; it does **not** prove the node's containerd cache was initially empty, certify the upstream image's source, or establish a fully reproducible build. For cold-node acceptance, separately record the node's pre-test image inventory, compare published registry digests with the approved source receipt, and test actual worker registration and workload recovery. Confirm effective `registries.yaml` and K3s embedded-registry enrollment on each node before relying on peer-to-peer mirrors. The validator does not change registry settings, restart agents, or activate any deployment.

### Compute controller Helm activation

The controller holds project and worker identities in a node-local SQLite database. Treat a controller image change as a stateful release: verify the immutable image digest, retain the live persistent volume and its owner node, and capture a restricted, consistent SQLite snapshot together with its artifact directory. A raw copy of a database in WAL mode is not a consistent snapshot. Verify the snapshot with `PRAGMA integrity_check` and keep it separate from the live volume. Do not start a second controller against the same database.

The chart disables both executable roles by default. A controller release created with an older chart may not record `controller.enabled` in its values. **Always set `controller.enabled=true` and `worker.enabled=false` explicitly** when upgrading it. Helm 3.20 or later can merge current chart defaults with stored release overrides using `--reset-then-reuse-values`; using `--reuse-values` alone can omit newly introduced values. A successful dry run is insufficient if its rendered manifest is empty.

For an existing controller release, substitute the verified source-owned chart directory and exact registry digest into this command. Run it with `--dry-run=server --hide-secret` first, then check that the manifest contains exactly the controller ServiceAccount, existing PVC, Service, and Deployment. Compare the proposed PVC name, storage request, storage class, Saturn/node placement, `Recreate` strategy, secret references, and environment with the live release. Resolve all differences before removing the dry-run flags.

```sh
helm upgrade pwm-controller "$WORKSPACE_HELM_CHART" \
  --namespace pwm-system \
  --reset-then-reuse-values \
  --set controller.enabled=true \
  --set worker.enabled=false \
  --set-string "controller.image=$CONTROLLER_IMAGE_DIGEST" \
  --atomic --wait --timeout 5m --history-max 10
```

Keep the preceding Helm revision and the verified state snapshot until the new controller passes authenticated project and worker enumeration, heartbeat freshness, read-only resource planning, workspace creation, command execution, workspace destruction, and access-denial checks. If a rollback is required after the database schema has changed, inspect compatibility and restore from the separately held pre-upgrade snapshot only with the controller stopped. A Helm rollback alone does not restore data.

Compute toolchain environments can be built locally with:

`bash services/compute/scripts/build-toolchain-image.sh <approved-image-reference> --load`

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

## Acceptance and rollout order

Every stage requires an exact source revision, verified release provenance, successful
relevant Go/Rust/Nix checks, preserved private state, and a tested rollback path.
Failure of a required check blocks that stage; unrelated services remain active.

1. **Network and security backbone.** Qualify routing, firewall boundaries,
   trusted management access, Cloudflare Tunnel and owner-only Access policies,
   signed identity verification, Rust authority and executor isolation, secret
   custody, unauthorized-request rejection, and recovery after controlled reboot.
   Preserve the existing router configuration and an independent recovery path.
2. **Unified MCP.** Verify the public MCP route, authenticated initialization,
   tool discovery, exact domain allowlists, private backend delegation,
   unauthorized-write rejection, CSRF/Origin protection where applicable,
   session expiry, and service restart recovery. Retain independent domain
   storage and avoid granting deployment privileges to the gateway.
3. **Compute and cluster.** Publish worker images by immutable digest through a
   durable authenticated registry, reconcile K3s worker configuration one node
   at a time, and verify fresh-node pulls, worker registration, workspace
   lifecycle, hardware custody, job recovery, and five-node readiness. Do not
   rely on an image cached by a single node.
4. **Rust/Dioxus console.** Accept Cloudflare owner sessions and required
   browser operations before replacing Homepage. Verify the pinned shadcn/ui
   component contracts, accessibility, light and dark themes, responsive
   layouts, and authenticated application parity. Retain Homepage as a recovery
   interface until these checks pass.
5. **Remaining services and retirement.** Validate the independent Learning,
   Health, media, monitoring, calendar, and other domain applications, then
   retire superseded infrastructure only after its replacement has passed
   acceptance. Preserve media files and every rollback artifact still required
   for essential infrastructure.

Promote the tested NixOS generation to the boot profile only after a reversible
activation trial succeeds. A selected boot generation is not proof of cold-boot
recovery. Do not delete private databases, previous application deployments,
existing repositories, or recovery points merely because a newer build exists.

## Canonical resource references

The platform uses references of the form `homelab:<kind>:<id>`, where kind is `task`, `goal`, `recipe`, `meal`, or `book`. `resource_resolve` and the native `GET /v1/resources/resolve?ref=...` route resolve a reference through the owning first-party service rather than copying application records into a shadow store. Invalid or unsupported identities fail closed. Compute project and research-library references remain owned by their service until an authenticated cross-service resolver is implemented.

## Local monitoring

Monitoring reuses the private Prometheus and Loki services. The platform's monitoring adapter accepts only loopback origins, fixed PromQL expressions and bounded Loki journal queries. The Rust dashboard can replace the Grafana presentation after compilation and acceptance. The collectors, retention rules and alert evaluation remain independent of the frontend.

## Engineering-run boundary

The independent `homelab-engineeringd` package and NixOS module are staged as part of the unified release. The daemon is opt-in. It requires protected project-scoped compute credentials and a finite list of allowed projects. An optional source model has separate credentials. The gateway receives neither secret; it can submit, inspect or cancel an engineering run only through the local Unix lifecycle socket.

An engineering run may execute isolated source tasks and request a revision-guarded draft pull request. It cannot merge, change the NixOS fleet, restart critical services, or bypass the independent privileged executor. See [Engineering execution](engineering.md).
