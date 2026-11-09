# Homelab architecture

Homelab exists to strengthen the owner's ability to reason, learn and produce consequential work. See [the governing philosophy](philosophy.md). Architecture is subordinate to that purpose; additional services must earn their cognitive and operational costs.

## One system

Homelab is one personal computing system. Its owner interacts with a single catalog of capabilities, operations and resources through a common Go API and MCP endpoint. Tasks, goals, projects, Git repositories, shared libraries, learning evidence, health records, computing resources and media may refer to each other using stable identifiers. A project is not defined by its Git repository. Project overview resources join controller-owned context, linked repositories, linked libraries, explicitly linked personal tasks and their goals, and engineering run summaries by exact project ID. Missing downstream services remain visibly unavailable; no global shadow database is constructed.

The `system/` tree is the application's source of truth. It contains a primary Go module and independently buildable implementation modules. Module boundaries exist for reliability, toolchain constraints and distinct state ownership; they must not become separate assistant identities, duplicate records or inconsistent interfaces. Go operations remain usable without an AI client. The Rust/Dioxus console invokes those same native operations through narrowly scoped owner-session routes.

Nix provides the root flake, host configuration, deployment definitions, credential loading and component activation. Saturn runs routing, the K3s control plane and persistent lightweight applications. Jupiter, Venus, Mercury and Janus execute authorized compute work. Specialized processes may be upgraded separately, but the user-facing interface remains one system. One process is not required for one product.

## Capabilities

The system implements personal planning, recurrence-aware calendar availability, advisory weekly allocation, task reports, compute and Git workspaces, research libraries, adaptive learning, health and nutrition, media requests, and infrastructure observation. Its custom Rust monitoring views consume a bounded Go adapter over Prometheus metrics and Loki journal data; established collectors retain responsibility for telemetry and historical storage. Quantitative finance is not active. Seerr owns movie and television requests; the assistant exposes those capabilities without duplicating Seerr's database.

A single capability registry and consistent authorization rules govern tool discovery and execution. Operations use native Go interfaces. An MCP transport is an adapter rather than the business-logic owner. The platform must distinguish a successful operation from one merely queued, accepted or unobserved.

## Engineering execution

Engineering runs are managed by an independent Go daemon within the same product. The public MCP and native API expose owner-authorized run submission, status and cancellation through a restricted Unix socket. Compute and model credentials never enter the gateway. The daemon schedules project-scoped, resource-bounded work and relies on an authenticated compute controller for isolated workspaces, exact-revision source checkout and worker-confirmed operations. Only verified artifacts may be sent to revision-guarded draft pull-request publication; merge and deployment remain separate operator actions. See [Engineering execution](engineering.md).

## Hardware and privilege

The declared inventory is in `hardware.nix`, validated by the Go `system/devices` package and presented through the native API and MCP. An inventory entry is not proof of physical connectivity. The current Kria KV260 inventory entry permits SSH connection and inspection only and is not compute-eligible. Any physical device can be removed or reconfigured through the inventory without model-specific Go changes. The compute executor must enforce actual device selection and lease authorization independently of the gateway.

Choose implementation languages by responsibility: **Go** owns service APIs, persistence, controllers, integration adapters, scheduling and business rules; **Rust** owns Dioxus/WASM interfaces and performance-critical hardware, accelerator and device code where profiling or memory-safety requirements justify it; **Nix** owns builds, services, fleet configuration and reproducible deployment. SQL, HTML and CSS remain the appropriate database/query and browser presentation formats. Do not rewrite established Go services in Rust without a concrete technical benefit. SSH-mode equipment is not a NixOS deployment target and cannot receive managed compute leases. SSH connections use unprivileged user accounts and require separately authorized execution. Enrolled NixOS hosts are derived from the declarative node inventory, not hardcoded in the flake.

The main assistant process never owns deployment or unrestricted cluster credentials. A separately trusted executor must authorize sensitive operations at the actual enforcement boundary. External credentials and media files remain outside Git.

## Deployment

Local release staging and project-scoped OCI image-building source are included, but operator-managed credentials, protected service activation and live acceptance remain separate responsibilities. Source availability is not evidence of successful deployment. Builds, Nix evaluations, live authentication, state continuity, capability parity and rollback require verification before cutover. The active fleet and media remain unchanged until that acceptance succeeds.
