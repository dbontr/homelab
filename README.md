# Homelab

Homelab is a self-hosted, adaptive personal capability system. It increases the owner's intellectual independence and useful work output while removing low-value administrative burden. Its architecture and priorities evolve with the owner's goals. See [philosophy](docs/philosophy.md) and [skills](skills/README.md).

The system is maintained in one repository but runs as independently operable services. The Rust/Dioxus UI, single aggregated MCP endpoint, and optional external API are separate presentation and integration surfaces. None owns the domain databases, background jobs, or privileged execution state. Nix declares their deployment and credentials.

## Source layout

- `skills/` contains portable Development, Writing, Teaching and Ideas skills for consistent assistant behavior and project workflows.

- `services/` contains the domain service implementations and private contracts. `services/core/` contains the application runtime; `services/compute/`, `services/learning/`, and `services/health/` are independently buildable modules. Each service retains its own persistent state.
- `ui/` contains the Rust/Dioxus console and its independent browser interface.
- `mcp/` contains the independently startable, single aggregated MCP listener.
- `api/` contains the independently startable, optionally writable native HTTP API listener.
- `infrastructure/` declares the hosts, networking, K3s, storage, media applications and operational environment.
- `nixosModules/` and `packages/` provide reproducible NixOS packages and activation.
- `hardware.nix` declares nodes and devices; `flake.nix` derives enrolled NixOS hosts and pins reproducible dependencies through `flake.lock`.
- `docs/` records current architecture, security, operating procedures and engineering standards.

The unified interface supports project-scoped Engineering runs for isolated source changes, verification and draft review, together with tasks, goals, configurable personal rules, quick-win recommendations, weekly progress reporting, bounded recurrence-aware calendar availability, advisory weekly task planning, on-demand book discovery, private acquisition requests and a canonical record-derived activity feed. Movie and television search and requests use Seerr when configured. Additional capabilities are attached through Go interfaces. Projects can link multiple repositories and shared libraries; a repository does not define a project.

Cross-capability research and explicitly authorized personal-task workflows are available through one authenticated API and MCP interface. Workflow steps use the existing durable, fenced executor. Owner-submitted task creation requires separate write consent and an atomic retry receipt; model-proposed stages remain read-only and receive no infrastructure mutation credentials. Project overviews join task/goal links and engineering summaries with canonical controller-owned project context. See [cross-capability workflows](docs/capability-workflows.md).

Compute, teaching and health implementations retain independent operating-system and database boundaries where necessary for reliability. Their separation must not require the user to choose between assistants, endpoints or inconsistent project identifiers.

See [interface boundaries](docs/interface-boundaries.md) for service ownership, availability and credential isolation. The [language policy](docs/language-policy.md) specifies Go, Rust, Nix and SQL responsibilities and the acceptance requirements for high-assurance migrations. The MCP and public API listeners are disabled by default and are not prerequisites for browser or background operation.

## Console

The Rust/Dioxus browser client is in [`ui/console`](ui/console). It uses shadcn/ui neutral design tokens and consumes authenticated native API endpoints for monitoring, workspace inspection, task and recipe workflows, health, teaching, calendar and planning. It does not ship React or embed privileged MCP credentials. See [console architecture](docs/console.md), [observability](docs/observability.md), [books](docs/books.md) and [self-hosted releases](docs/self-hosted-release.md).

## Development

Compute profiles are stored with immutable revisions, resource requirements and operator-approved software identifiers; profile resource planning is advisory and never schedules a job. SSH-mode equipment uses ordinary-user inspection probes without remote agents. The native tool-call API and MCP dispatch share the same application authorization layer. Credential encryption and independent executor grants exist as library contracts but have no live account or cluster-authority configuration.

The primary Go module is in `services/core/`. Compute, Learning and Health are independent Go modules under `services/`, composed by the repository Go workspace. Build the primary runtime with:

```sh
cd services/core
go test ./...
go vet ./...
go build ./cmd/homelab-platform
```

The self-hosted `scripts/stage-release.sh` builds the integrated Go services, including the independent engineering daemon, and a Dioxus web bundle into a new, checksummed release directory without changing running services. The internal Kubernetes compute implementation includes a rootless registry image builder and a locally callable `services/compute/scripts/build-toolchain-image.sh` for Go/Rust/WASM toolchain images. NixOS operators can evaluate enrolled hosts and build the `homelab-platform` package from the root flake. Source development never activates a generation. Runtime credentials, personal state and media bytes are not stored in Git.

## Operating boundaries

The public-facing MCP does not inherit deployment or cluster administrator credentials. Sensitive operations require short-lived operator approval and separate enforcement at the actual privileged executor. External accounts use protected local credentials or provider authentication. The current KV260 entry is configured for SSH connection and inspection only. Device permissions and compute eligibility are individually declared in `hardware.nix`, rather than hardcoded by board model. SSH mode is agentless, while enrolled mode permits explicitly authorized managed compute.

Source code in this repository is not itself proof of an active deployment. The existing machines and media remain authoritative until build, authentication, integration and rollback acceptance passes. Consult [engineering execution](docs/engineering.md), [architecture](docs/architecture.md), [hardware](docs/hardware.md), [security](docs/security.md), [interfaces](docs/interfaces.md), [deployment](docs/deployment.md), [engineering standards](docs/engineering-standards.md) and [references](REFERENCES.md).
