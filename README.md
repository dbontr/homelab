# Homelab

A self-hosted personal computing system managed through one source repository. Go implements its operations, MCP and native HTTP interfaces. Nix declares the operating environment. Every capability is available through the same assistant interface; an MCP client is optional.

## Source layout

- `system/` contains the entire Go application source: personal planning, research execution, computation, learning, health, media, credentials and integrations. Internal Go modules preserve independent build and failure boundaries, not separate user-facing products.
- `infrastructure/` declares the hosts, networking, K3s, storage, media applications and operational environment.
- `nixosModules/` and `packages/` provide reproducible NixOS packages and activation.
- `hardware.nix` declares nodes and devices; `flake.nix` derives enrolled NixOS hosts and pins reproducible dependencies through `flake.lock`.
- `docs/` records current architecture, security, operating procedures and engineering standards.

The unified interface supports tasks, goals, configurable personal rules, quick-win recommendations, weekly progress reporting, bounded recurrence-aware calendar availability, advisory weekly task planning, on-demand book discovery, private acquisition requests and a canonical record-derived activity feed. Movie and television search and requests use Seerr when configured. Additional capabilities are attached through Go interfaces. Projects can link multiple repositories and shared libraries; a repository does not define a project.

Compute, teaching and health implementations retain independent operating-system and database boundaries where necessary for reliability. Their separation must not require the user to choose between assistants, endpoints or inconsistent project identifiers.

## Console

The Rust/Dioxus browser client is in [`system/console`](system/console). It uses shadcn/ui neutral design tokens and consumes authenticated native API endpoints for monitoring, workspace inspection, task and recipe workflows, health, teaching, calendar and planning. It does not ship React or embed privileged MCP credentials. See [console architecture](docs/console.md), [observability](docs/observability.md), [books](docs/books.md) and [self-hosted releases](docs/self-hosted-release.md).

## Development

Compute profiles are stored with immutable revisions, resource requirements and operator-approved software identifiers; profile resource planning is advisory and never schedules a job. SSH-mode equipment uses ordinary-user inspection probes without remote agents. The native tool-call API and MCP dispatch share the same application authorization layer. Credential encryption and independent executor grants exist as library contracts but have no live account or cluster-authority configuration.

The core Go module is in `system/`; specialist executables and packages are in its compute, learning and health implementation directories. The Go workspace at the repository root composes those modules. Build the core using the Go toolchain from `system/`:

```sh
cd system
go test ./...
go vet ./...
go build ./cmd/homelab-platform
```

The self-hosted `scripts/stage-release.sh` builds Go service binaries and a Dioxus web bundle into a new, checksummed release directory without changing running services. The imported Kubernetes Workspace module includes a rootless registry image builder and a locally callable `system/compute/scripts/build-toolchain-image.sh` for Go/Rust/WASM toolchain images. NixOS operators can evaluate enrolled hosts and build the `homelab-platform` package from the root flake. Source development never activates a generation. Runtime credentials, personal state and media bytes are not stored in Git.

## Operating boundaries

The public-facing MCP does not inherit deployment or cluster administrator credentials. Sensitive operations require short-lived operator approval and separate enforcement at the actual privileged executor. External accounts use protected local credentials or provider authentication. The current KV260 entry is configured for SSH connection and inspection only. Device permissions and compute eligibility are individually declared in `hardware.nix`, rather than hardcoded by board model. SSH mode is agentless, while enrolled mode permits explicitly authorized managed compute.

Source code in this repository is not itself proof of an active deployment. The existing machines and media remain authoritative until build, authentication, integration and rollback acceptance passes. Consult [architecture](docs/architecture.md), [hardware](docs/hardware.md), [security](docs/security.md), [interfaces](docs/interfaces.md), [deployment](docs/deployment.md), [engineering standards](docs/engineering-standards.md) and [references](REFERENCES.md).
