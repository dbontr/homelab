# Homelab

A self-hosted personal computing platform implemented in NixOS and Go. Source code, infrastructure, and deployment definitions are maintained together. The system presents one personal assistant interface without making the operation of any service dependent on ChatGPT or the MCP transport.

## Repository layout

| Path | Responsibility |
| --- | --- |
| `flake.nix` | Declarative configuration for Saturn, Jupiter, Venus, Mercury and Janus |
| `infrastructure/` | NixOS routing, networking, diagnostics, media and host configuration |
| `services/workspace/` | Distributed compute, Git projects and libraries, hardware and artifacts |
| `services/learning/` | Adaptive teaching and knowledge evidence |
| `services/health/` | Health, nutrition, training and RecipeSage integration |
| `platform/` | Unified native API, personal planning, weekly reporting and primary MCP gateway |
| `nixosModules/` | NixOS integration for the platform application |

The Go platform uses a private, atomic JSON state store for one owner. It supports tasks, goals, personal rules, quick-win recommendations, weekly reports and a read-only iCalendar subscription. A minimum of three completed tasks per day counts tasks regardless of size; long-term goal progress is measured separately. Calendar events with recurrence rules are returned as definitions and are not yet expanded into dated occurrences.

Existing Go services remain buildable as separate modules in a Go workspace. The gateway can discover and invoke their MCP tools behind one authenticated endpoint when their private URLs are configured. Their runtime identities, permissions, and stores remain independently protected.

## Development

The platform requires Go 1.23 or later and uses only the Go standard library:

```sh
cd platform
go test ./...
go vet ./...
go build ./cmd/homelab-platform
```

The root Nix flake builds the Go platform directly with `nix build .#homelab-platform` and composes all five hosts. The complete Go workspace requires the highest Go version declared by its constituent modules. Use the pinned Nix toolchain when evaluating infrastructure and application releases.

## Operations

The platform service is defined in `nixosModules/platform.nix` and is disabled by default. When enabled it binds to loopback, requires a protected owner token, stores state outside Git, and can read a Proton Calendar sharing URL from a systemd credential. Its native API and MCP endpoint are not directly exposed to the network. The development and release pipeline is separate from the main MCP's operational capabilities.

Source consolidation is not equivalent to runtime acceptance. The existing operating services remain authoritative until the integrated deployment has passed build, authorization, recovery, and cutover checks. No media files or running deployments are modified by source imports.

See [architecture](docs/architecture.md), [interfaces](docs/interfaces.md), [deployment](docs/deployment.md), and [references](REFERENCES.md).
