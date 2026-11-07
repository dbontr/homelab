# Homelab project

## Objective

Maintain a reproducible, recoverable personal platform for network services, media, recipes, learning and scheduled computation. Saturn must remain usable when every optional compute worker is unavailable.

## Ownership

The homelab repository owns NixOS host composition, topology, access boundaries and network routing. Each MCP repository owns its implementation, service module, packaging and release policy. The system is divided into six domains: network/security, diagnostics/observations, media/downloads, learn stack, workspace MCP, and personal health stack. Shared portal and recovery facilities do not define additional application domains. Infrastructure uses Nix and Bash, not custom Python services. The Go services [Workspace MCP](https://github.com/dbontr/workspace-mcp), [Adaptive Teaching MCP](https://github.com/dbontr/adaptive-teaching-mcp) and [Personal Health MCP](https://github.com/dbontr/personal-health-mcp) own their application behavior, packaging and tests. Diagnostics and Media MCPs require separately scoped implementations.

## Invariants

- Saturn remains the router, core-service host and K3s control plane.
- Independent domain MCPs use distinct credentials, access policy and knowledge banks.
- Workers receive project-scoped capabilities, never host-wide administrative tokens.
- Cloudflare Access, VLAN isolation and fail-closed download VPN routing remain enforced.
- GitHub governs reviewed configuration and, after migration, project knowledge; databases and bootstrap secrets are separate.
- Application fixes are published in the owning upstream release before deployment pins change.
- Recovery must be possible from verified encrypted state and independently protected bootstrap identities.

## Acceptance

All host configurations evaluate; an isolated restore succeeds; each service passes authorization, data continuity, worker-failure and rollback checks before it replaces the operating integration.
