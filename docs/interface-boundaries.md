# Interface and service boundaries

Homelab consists of one collection of independently operated services and three presentation surfaces. One repository provides a common release and declarative configuration; application state is not shared by making each surface an owner of the same data.

## Ownership

| Directory | Responsibility |
| --- | --- |
| `services/` | Long-running domain services and private inter-service transport contracts |
| `services/core/` | Go application runtime, personal data persistence, scheduling, security, native resource handlers and the canonical capability catalog |
| `services/compute/`, `services/learning/`, `services/health/` | Independent service modules and data/failure domains |
| `services/motivation/` | Independent Rust owner-defined motivation, experiments, feedback and state ownership |
| `ui/` | Rust/Dioxus client assets and the independent same-origin UI ingress |
| `mcp/` | Optional single MCP listener; discovery and tool calls delegate to the service runtime |
| `api/` | Optional native HTTP listener; read-only unless exact write resources are configured |
| `infrastructure/`, `nixosModules/`, `packages/` | Host and cluster resources, process and credential isolation, builds and release policy |

The optional `homelab-calendar` daemon owns the external calendar sharing-link credential and serves only authenticated, read-only calendar feed data over a loopback listener. The platform uses the same bounded feed source for native views, MCP tools and scheduling, and continues without calendar availability if the daemon stops. The service can run without the UI, MCP or public API.

Moonfin/Seerr, the monitoring stack, K3s, and service-specific SQL persistence remain services, not gateway plugins. NixOS can operate these components without a web UI, an MCP client or an external API.

## Data and credentials

The service runtime is the authoritative writer for its own state. Interface processes open no project, task, recipe, health or monitoring database and schedule no background jobs. They only pass validated HTTP requests to the owning service. SQL tables belong to an application service, not to an MCP or UI adapter.

The platform acquires nonblocking operating-system file locks for the task store, recipe store, profile registry and workflow state before opening those resources. A competing cooperative process fails startup rather than acquiring a second writer. Lock files are private (`0600`), symbolic-link resource paths and lock files are rejected, and the kernel releases ownership on process exit. The locks are advisory: any future split service that accesses these resources must participate in the same ownership protocol, or use an authenticated service operation instead of opening the files directly. A separate process cannot become a writer merely by acquiring a different lock name.

The independent NixOS UI uses same-origin private resource and owner-session routes. It does not insert a service administrator bearer into browser requests. The MCP and API listeners each authenticate a distinct external bearer and replace it with a separately configured service transport credential when requesting private operations. Caller-controlled authorization cookies and forwarding headers never cross that boundary. Private service responses cannot set public-interface cookies or override no-cache, content-type, referrer, and framing headers. Response headers are bounded; oversized upstream headers cause an explicit unavailable response. Interface token files are opened without following symbolic links and checked on the opened file descriptor. The domain runtime enforces separate MCP-only and API-only credentials, checks API write-path scopes again, and rejects tool calls made using the API service identity. Write requests through the optional API are denied unless the specific native path prefix is configured; privileged tool invocation is unavailable through that API. The unified MCP reports an `accepted` state for a privileged request that can still be in progress and a `completed` state only after the independent host release controller has verified its transition.

A service backend address is fixed at process startup and must be a numeric loopback HTTP origin. The public listener also binds loopback. The platform's administrator bearer is not loaded into the MCP or API processes. Without an independent owner-security service, privileged operations are unavailable; administrator-only backend tools cannot be forwarded directly. Explicit ingress configuration, TLS, allowlisted origins and owner authentication remain separate infrastructure responsibilities.

## Availability

| Unavailable component | Independent behavior |
| --- | --- |
| MCP listener | Service runtime, UI and external API remain available |
| External API listener | Service runtime, UI and MCP remain available |
| Rust console or Nginx UI listener | Long-running services, MCP and external API remain available |
| One specialist service | Other services continue; its resources are reported unavailable |
| Rust motivation service | Personal tasks, research, learning, UI, MCP and native API remain independently operable; motivation routes report unavailable |
| Domain service runtime | Its clients receive an explicit unavailable response; no interface becomes the new domain writer |
| Monitoring services | Existing work continues but its telemetry views become unavailable |

The Go core runtime in `services/core/` owns the internal MCP dispatcher and authenticated router. Its motivation adapter forwards only fixed requests with the private Rust service identity; profile and intervention state never become Go-owned data. The `services/core/internal/personal/` package registers task, goal, rule, recommendation and weekly-report routes against the canonical personal state store. `services/core/internal/monitoring/` owns fixed read-only monitoring views over independent Prometheus and Loki instances, while `services/core/internal/mediahttp/` owns restricted Seerr search and request routes. All three use one bounded JSON request/response contract, and the core router still enforces owner authentication. This separates the personal HTTP contract from the transport dispatcher without creating another database writer. Book discovery and request endpoints are registered by `services/core/internal/bookhttp/` against the same canonical store and optional catalog. A catalog outage does not prevent the owner from listing or updating existing book requests. Compute profile revisions and their native HTTP and MCP operations are owned by `services/core/profiles/`; the central transport retains only Workspace-backed resource planning. Calendar feed and recurrence views are owned by `services/core/internal/calendarhttp/`, while hardware inventories are served from `services/core/devices/`. These remain read-only and are registered beneath the same owner-authorization boundary. Other domain routes remain under the core transport until they have equivalent service-owned contracts. The independent listeners isolate external lifecycles and credentials, not yet every package dependency. Domain handlers can be moved into their respective service modules by preserving their internal contracts and changing one call site at a time. Such moves must retain compatibility with persisted records and existing owner-session behavior.

## Source verification

`scripts/verify-offline.sh` checks the standalone Go interface packages with race tests and vet, alongside the existing service modules. `scripts/stage-release.sh` packages separately executable MCP and API binaries and the Rust UI assets. `scripts/verify-release.sh` verifies those artifacts as part of the exact source revision's checksum manifest. No interface is enabled solely by building or staging it.
