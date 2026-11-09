# Engineering standards

## One authoritative implementation

The repository contains a unified personal computing system. A module is an implementation boundary, not a separate product. The codebase must have one canonical model for projects, tasks, capabilities, permissions and resource references. Every native API and MCP operation invokes the same underlying Go logic.

- `services/core/`: Go application runtime, native resource handlers and persistence; `services/compute/`, `services/learning/` and `services/health/`: specialist services; `mcp/` and `api/`: optional external interfaces.
- `infrastructure/`: NixOS hosts, network policy, storage, K3s, media software and diagnostics. The `homelab/default.nix` entrypoint composes the host configuration.
- `hardware.nix`: declarative node and device registry with explicit management modes. Runtime credentials and connection secrets are never included.
- `packages/`, `nixosModules/`: reproducible packages and NixOS activation.
- `flake.nix`, `flake.lock`, `go.work`: dependency and build composition.
- `docs/` and `REFERENCES.md`: current technical contracts, procedures and primary sources.

A project may include several repositories, shared libraries, learning goals, experiments and tasks. GitHub repository names are locators, not immutable project identifiers. Shared identifiers do not require all records to reside in one database.

## Engineering discipline

1. Determine the canonical owner and consumer of a change before editing. Avoid duplicating handlers, state, settings or near-identical operations.
2. Use Go and Nix by default. Introduce Rust only where a bounded hardware, memory, or WebAssembly component benefits materially; package it through Nix and keep its interface stable.
3. Do not use one executable or database merely for visual uniformity. Isolate failures, sensitive data, external provider credentials and hardware leases.
4. Preserve a single API and MCP identity. Internal package and process names must not force the owner to select different assistants.
5. Manage hardware through declarative node and device entries. SSH-mode equipment is agentless and ineligible for managed compute leases; enrolled equipment can use managed workers under explicit permissions. Device authorization does not depend on manufacturer or model. Actual device operations and leases require executor-side enforcement.
6. Keep the privileged executor and deployment pipeline separate from the public assistant. Require operation-specific authorization at the executor, not only the gateway.
7. Prefer standard libraries and existing implementations. Delete obsolete files, unnecessary scaffolding, duplicate descriptions, temporary data, generated assistant metadata and abandoned configurations.
8. Maintain durable behavior tests, negative authorization tests and reference documentation. Format Go source with `gofmt`; validate Nix expressions and the actual build graph.
9. Preserve real media files and unique runtime state during all deployments. A Git commit does not constitute an activated release.
10. Use independently verifiable build, test, staging, activation, health-check and rollback steps before changing production infrastructure.

## Verification

Run `go test ./...`, `go test -race ./...`, and `go vet ./...` in `services/core/` and test the independent specialist modules with their declared toolchains. Evaluate every enrolled NixOS host configuration from the repository root. Verify MCP discovery, authorized and unauthorized calls, native APIs, media requests, remote compute, hardware restrictions and service rollback.

Keep structural boundaries only when they serve a real technical constraint. User-visible unity is determined by consistent behavior and ownership, not by the number of source files or running processes.
