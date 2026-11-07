# Personal Computing Platform

A NixOS and Go monorepo for a single-user, self-hosted computing environment. The system integrates infrastructure, personal planning, research execution, learning, health, media, and observability behind one authenticated API and MCP interface. Each capability remains usable independently of the MCP transport.

## Design

- One repository owns application source, host configuration, build definitions, and deployment manifests.
- Nix declares the five NixOS hosts and their services; Go implements application logic.
- The personal model is unified: people, goals, projects, resources, tasks, schedules, observations, and events can reference each other through stable identifiers.
- Business operations are exposed through Go interfaces. HTTP and MCP are adapters, not authoritative implementations.
- Stateless services may be replaced independently; persistent storage remains scoped to its authoritative owner.
- Saturn retains routing, identity, application services, and K3s control plane. Worker nodes execute isolated compute workloads.
- Cloudflare remains infrastructure plumbing and is not an MCP capability.
- The release controller is external to the primary MCP and validates revisions before applying NixOS or Kubernetes changes.
- Media files are outside Git and are never discarded during deployment.
- No local language model is required; AI reasoning is provided by a connected client.

## Implementation

Application source and infrastructure migrate into this repository without altering the running system. A release becomes authoritative only after acceptance testing and an explicit service cutover. Original repositories may be retired after the new repository serves their functionality.

See [architecture](docs/architecture.md), [interfaces](docs/interfaces.md), [deployment](docs/deployment.md), and [references](REFERENCES.md).
