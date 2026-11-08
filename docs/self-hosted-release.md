# Self-hosted release

The repository is the source of truth for the Go platform, Workspace Compute, Health, Adaptive Teaching and the Rust/Dioxus console. The release process is independent of GitHub Actions and never grants deployment credentials to the primary MCP gateway.

## Release staging

Use an operator-controlled host with the Go toolchains required by the four Go modules, Rust, Cargo, the WebAssembly target and Dioxus CLI available. The output directory must be new, and its parent must already exist.

`bash scripts/stage-release.sh /srv/releases/homelab/<unique-release-id>`

The script builds the six Go executables from local sources, packages the Dioxus web assets, and writes `SHA256SUMS` and `RELEASE`. It uses an unpublished temporary directory and renames it on completion. It does not alter NixOS generations, start services, publish images, stop the existing homepage, or touch application databases.

Workspace toolchain environments can be built locally with:

`bash system/compute/scripts/build-toolchain-image.sh <approved-image-reference> --load`

Use `--push` only when an operator has configured an approved registry, narrow package-write credential and verified image digest. Rootless BuildKit Kubernetes jobs are separately opt-in in the Workspace module. Publishing an image does not enroll it into a worker or activate a Kubernetes release.

## Activation

The Go platform and Rust console have separate NixOS activation switches. They remain disabled unless an operator enables them and supplies the required protected credentials. The existing Homepage dashboard stays active until a release is accepted and the console's explicit `replaceHomepage` option is enabled.

The service binaries for Workspace, Adaptive Teaching and Health retain their own state and release ownership. The imported source in this repository does not automatically replace their active binaries or modify Kubernetes controllers. An operator must select locally built, digest-qualified artifacts in the relevant service definitions and provide a rollback plan for each replacement.

## Acceptance before activation

1. Confirm the staged binary and asset SHA-256 manifest and the corresponding source revision.
2. Validate the Go modules, Rust/WASM build, NixOS host evaluations and service package derivations against the selected revision.
3. Confirm the owner browser session, MCP authentication, backend discovery, private monitoring access, calendar read-only behavior and blocked unauthorized actions.
4. Confirm worker image admission, registry push/pull, state retention, hardware identity and Kubernetes Job lifecycle independently of the public gateway.
5. Inspect existing media, DNS, routing and Cloudflare paths, and preserve access to Moonfin and RecipeSage before any cutover.
6. Activate one service at a time, verify readiness and retain the previous NixOS generation and immutable release. Avoid changing home routing until every dependent service is accepted.

Source staging is not evidence of these acceptance checks. Do not delete databases, previous application deployments, existing repositories, recovery points or media merely because a newer source build exists.

## Local monitoring

Monitoring reuses the private Prometheus and Loki services. The platform's monitoring adapter accepts only loopback origins, fixed PromQL expressions and bounded Loki journal queries. The Rust dashboard can replace the Grafana presentation after compilation and acceptance. The collectors, retention rules and alert evaluation remain independent of the frontend.
