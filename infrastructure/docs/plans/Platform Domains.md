# Service architecture

## Operational domains

A pinned NixOS flake defines Saturn and four K3s workers. Kubernetes schedules Project Workspace tasks; Saturn retains network, security, persistent application services and the control plane. Host domains compose the network, credentials and application placement. MCP repositories own their NixOS service modules, Helm charts where applicable, runtime policy and release artifacts.

| Domain | Configuration | Scope |
| --- | --- | --- |
| Network & Security | `homelab/domains/network-security.nix` | Router, VLANs, DNS, firewall, Cloudflare, remote administration |
| Diagnostics & Observations | `homelab/domains/diagnostics-observations.nix` | Prometheus, Grafana, Loki, exporters, GPU metrics and alerting |
| Downloads & Media | `homelab/domains/downloads-media.nix` | Media, Seerr, Arr services and fail-closed VPN downloads |
| Learn Stack | `homelab/domains/learn.nix` | Adaptive Teaching service, learner SQLite and authenticated MCP |
| Workspace MCP | `homelab/domains/workspace-mcp.nix` | Upstream service import and site-specific controller configuration |
| Personal Health Stack | `homelab/domains/personal-health.nix` | RecipeSage application and PostgreSQL, Personal Health MCP and private health records |

The domain index is `homelab/default.nix`. `homelab/shared.nix` owns Homepage and shared application deployment mechanisms. Network & Security owns recovery configuration and disables automatic scheduling. `hosts/` owns the five machine configurations. `homelab/stacks/` contains upstream Compose and monitoring manifests, never application source.

## Runtime boundaries

- **Network & Security:** Preserve VLAN 10/20/30 isolation, Cloudflare Access and the downloader's VPN-only egress. No domain MCP receives broad router privileges.
- **Monitoring:** Prometheus/Grafana and existing exporters provide monitoring. Native shell telemetry is published through the node-exporter textfile directory. No custom Python monitoring API is deployed. Diagnostic MCP work, if introduced, belongs in a separate Go repository and is read-only by default.
- **Media & Downloads:** Jellyfin/Moonfin and Seerr operate independently from the VPN-confined Arr and download services. The private Seerr bridge exposes only authenticated Radarr/Sonarr APIs.
- **Adaptive Teaching MCP:** [Adaptive Teaching MCP](https://github.com/dbontr/adaptive-teaching-mcp) owns learning schemas, persistence, attachments, knowledge graph and assessment. Its database and credentials are protected state; the homelab pins the upstream binary and declares its service.
- **Project Workspace MCP:** [Project Workspace MCP](https://github.com/dbontr/workspace-mcp) owns the Go controller, Kubernetes workers and execution contract. Project source lives in the configured Git remote, GitHub on this installation. Optional project knowledge is Git-backed only when enabled. Controller SQLite and artifact bytes need separate recovery.
- **Personal Health:** RecipeSage PostgreSQL remains the application source of truth. [Personal Health MCP](https://github.com/dbontr/personal-health-mcp) integrates RecipeSage API operations and maintains a separate private health database. RecipeSage is installed within the Personal Health domain; only Personal Health exposes MCP tools.

The Workspace MCP repository maintains the Helm chart, NixOS deployment module and OCI image validation. Private release acquisition and namespace-scoped image credentials must be verified before activation. Adaptive Teaching uses an upstream verified service package. Neither a repository pin nor a successful Nix evaluation is proof of a running production instance.

## Infrastructure language and ownership

Use Nix for declarative system configuration and Bash with standard native tools for administrative and recovery scripts. Use built-in systemd, Kubernetes, Prometheus, SQLite, PostgreSQL and supported upstream APIs instead of custom Python daemons. General-purpose application implementations belong in independent Go repositories. The homelab repository has no tracked Python source files.

## Deployment gates

1. Evaluate each NixOS target and inspect generated services, networking, authentication and storage effects.
2. Check active services and classify retained state; do not delete unique data while automatic backups are paused.
3. Confirm upstream application release identity, image digests, scoped credentials and storage.
4. Validate authorized and unauthorized MCP behavior, persisted learner records, a representative project job, and media/recipe functionality.
5. Demonstrate independent restore of application databases, controller artifacts and bootstrap identities.
6. Switch one service at a time with documented rollback; retire superseded services only after acceptance.

NanoKVM remains an independent physical administration channel. A GitHub commit is not an activated NixOS generation.
