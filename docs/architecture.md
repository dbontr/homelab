# Architecture

## One system, multiple failure boundaries

The platform exposes a single personal operating model to its owner. Projects, goals, tasks, research outputs, learning evidence, health observations, computing resources, and media assets are related through stable identifiers. The user-facing distinction is by capability, not by application ownership.

All capabilities are implemented independently of their user-interface adapters. HTTP, MCP, and future native interfaces invoke the same Go operations; they do not define separate business logic. The primary MCP gateway aggregates capabilities without granting every process a shared administrator identity.

## Components

- The Nix flake composes five hosts from infrastructure/ and includes the locally owned Workspace source.
- The Go platform supplies personal tasks, goals, constraints, activity-derived reporting, a native API, and a primary MCP entry point.
- Workspace provides Git, project repositories and libraries, compute scheduling, artifacts, Kubernetes workers, and approved hardware allocation.
- Learning supplies adaptive teaching, knowledge graph and independent assessment.
- Health supplies longitudinal observations, fitness, nutrition, meal planning, and RecipeSage integration.
- Media and monitoring remain infrastructure-managed services.
- The finance capability is not active.

Saturn hosts networking, the K3s control plane, and persistent low-cost application services. Jupiter, Venus, Mercury, and Janus receive approved compute jobs. Stateful storage is managed at the service boundary rather than placed in a shared universal database.

## Resource identity

Resource references use stable identifiers independent of source repositories. A project can refer to many Git repositories, libraries, research artifacts, tasks, and learning goals. A repository does not define a project. Links across services must tolerate temporary unavailability without changing the underlying record.

## Integration and deployment

The gateway uses explicit, configured downstream service connections. Each backend retains its own authorization and state. Gateway unavailability does not prevent independently hosted services from operating.

Infrastructure deployment is owned by a separate release process, currently administered through NanoKVM. The primary MCP can request authorized operations but does not own its own deployment credentials or modify the release controller. Credentials are read from protected runtime files and excluded from Git.

The initial platform is an integration worktree, not a declaration that every imported component is production-ready. Source parity does not imply operational parity; native operations, MCP discovery, authorization, Nix evaluations, worker acceptance, and data continuity require independent tests.
