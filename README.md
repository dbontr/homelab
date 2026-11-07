# Homelab

A self-hosted personal computing system managed through one source repository. Go implements its operations, MCP and native HTTP interfaces. Nix declares the operating environment. Every capability is available through the same assistant interface; an MCP client is optional.

## Source layout

- `system/` contains the entire Go application source: personal planning, research execution, computation, learning, health, media, credentials and integrations. Internal Go modules preserve independent build and failure boundaries, not separate user-facing products.
- `infrastructure/` declares the hosts, networking, K3s, storage, media applications and operational environment.
- `nixosModules/` and `packages/` provide reproducible NixOS packages and activation.
- `flake.nix` and `flake.lock` define the five-host fleet and reproducible dependency revisions.
- `docs/` records current architecture, security, operating procedures and engineering standards.

The unified interface supports tasks, goals, configurable personal rules, quick-win recommendations, weekly progress reporting, and read-only iCalendar data. Movie and television search and requests use Seerr when configured. Additional capabilities are attached through Go interfaces. Projects can link multiple repositories and shared libraries; a repository does not define a project.

Compute, teaching and health implementations retain independent operating-system and database boundaries where necessary for reliability. Their separation must not require the user to choose between assistants, endpoints or inconsistent project identifiers.

## Development

The core Go module is in `system/`; specialist executables and packages are in its compute, learning and health implementation directories. The Go workspace at the repository root composes those modules. Build the core using the Go toolchain from `system/`:

```sh
cd system
go test ./...
go vet ./...
go build ./cmd/homelab-platform
```

NixOS operators can evaluate the five hosts and build the `homelab-platform` package from the root flake. Runtime credentials, personal state and media bytes are not stored in Git.

## Operating boundaries

The public-facing MCP does not inherit deployment or cluster administrator credentials. Sensitive operations require short-lived operator approval and separate enforcement at the actual privileged executor. External accounts use protected local credentials or provider authentication. A borrowed Kria KV260, if connected, is SSH-only; no physical FPGA access or modification is authorized by default.

Source code in this repository is not itself proof of an active deployment. The existing machines and media remain authoritative until build, authentication, integration and rollback acceptance passes. Consult [architecture](docs/architecture.md), [security](docs/security.md), [interfaces](docs/interfaces.md), [deployment](docs/deployment.md), [engineering standards](docs/engineering-standards.md) and [references](REFERENCES.md).
