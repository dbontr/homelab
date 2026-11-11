# Privileged operations and custody boundaries

Privileged authority is not conferred by an MCP tool, browser session, source workspace, HTTP request, or the presence of a service account. A dedicated owner security service approves exact actions. A separate host executor verifies approval and resource scope before running any privileged operation.

The optional local Rust engineering sandbox has no authorization to switch
NixOS generations or access host credentials. Native hardware identity
validation is separately selected by a compute worker under its Go-owned lease
and the immutable operator device policy. These two facilities do not inherit
the privileged executor's administrative authority.

## Process inventory

| Process or subsystem | Canonical implementation | Credential and authority boundary |
| --- | --- | --- |
| MCP and HTTP API | `mcp/`, `api/`, `services/core/privileged/client.go` | Unprivileged entrypoints. Submit typed requests to the executor socket. Never receive owner passwords, TOTP secrets, vault encryption keys, kubeconfig, or unrestricted root tokens. |
| Owner security authority | `services/security-authority/`; compatibility code `services/core/security/` | Owns password/TOTP checks, revocable sessions, signed owner validation, one-use destructive confirmations, and encrypted credential records. Owner-only browser and administrative paths remain separate from gateway and mTLS clients. |
| Host privileged executor | `services/executor/`; compatibility code `services/core/privileged/` | Owns authorization checks, resource policy, protected audit, private service credentials, and host-only privileged effects. Its Unix client group has no owner-login socket access. |
| Restricted command runner | `services/actuator/` | Accepts enumerated systemd and Kubernetes verbs, canonical resource identifiers and immutable operator allowlists. Does not expose a shell, arbitrary binary path, or filesystem mutation API. |
| Host NixOS release controller | `services/executor/src/release.rs`; compatibility code `services/core/release/` | Builds from clean exact revisions, owns the host release journal, switches approved NixOS generations, verifies health, and restores the previous generation after failure. |
| Fleet release coordinator | `services/core/release/remote_agent.go`, `nixosModules/fleet.nix` | Coordinates remote stages and activations through certificate-authenticated host executors. It does not replace a host executor's local policy, approval, or audit. |
| Compute and hardware workers | `services/compute/`, `nixosModules/engineering.nix` | Perform delegated workspace and device operations under their own scoped leases and credentials. Their enrollment does not imply host administrative authority. |
| Out-of-band recovery | `services/executor/src/bin/recovery.rs`, `services/executor/src/bin/audit-inspect.rs` | Root-only host console, mutually exclusive with the active executor's state lease. External KVM access remains independent of application authorization. |

## Accepted executor mutations

| Operation family | Resource check | Privileged effect |
| --- | --- | --- |
| `infrastructure__service_start/stop/reload/restart` | Exact host and Nix-allowlisted noncritical systemd unit | Fixed Rust actuator service verb |
| `cluster__deployment_restart/scale` | Exact host, namespace and deployment; bounded replica count and optional Kubernetes resource-version precondition | Fixed Rust actuator Kubernetes verb |
| `infrastructure__workspace_create/stop/build_environment/revoke_worker` | Exact host, independently configured project, worker and image-pool identities; bounded CPU, memory, accelerator and display claims | Fixed Workspace MCP tool with credential read only after approval |
| `infrastructure__workspace_register_image/register_software` | Exact host, image-pool and software identities, approved immutable image digest or approved package list | Register approved OCI images or software through the executor's protected controller credential |
| `infrastructure__workspace_propose_program/review_program/update_entry` | Exact host and allowlisted program, image or software identity; status transition and revision fencing; source URL and pool association validation | Submit or review software catalog records through the same protected controller credential |
| `infrastructure__release_stage` | Exact host and clean 40-hex Git revision | Offline Nix build and protected staged manifest |
| `infrastructure__release_activate/revert` | Exact host and 48-hex staged release ID, existing manifest, active generation and confirmation | Durable NixOS switch with local health checks and rollback |
| `infrastructure__release_status` and `infrastructure__audit_status` | Signed owner assertion and independent read authorization | Protected journal and unresolved-attempt observation only |

The Nix options `services.homelabExecutor.workspace.images`,
`imageReferences`, `software`, `programs`, `packages`, `workers`
and `projects` supply independent resource allowlists. All seven
workspace families require exact operation-bound owner authorization,
and catalog writes plus workspace creation require one-use confirmation.
The gateway neither reads the controller's privileged token nor forwards
an arbitrary privileged MCP tool to the controller. A release must verify
both Go and Rust policy implementations against the same deny fixtures.

Every mutation uses a canonical JSON object. The security authority binds authorization to the operation, target, arguments, authenticated owner and current session. Destructive confirmations are bound to one exact canonical digest and cannot be reused. A durable audit-start event precedes each external side effect. A completed command is not sufficient evidence that an asynchronous downstream job has completed.

## Socket and state ownership

The security service owns `/run/homelab-securityd/client.sock` and the inaccessible-to-gateway owner socket. The executor owns `/run/homelab-executord/execute.sock`; the platform joins only its constrained client group when explicitly configured. Optional remote authorization and release listeners use TLS 1.3, configured trust roots and exact certificate DNS identities. No listener accepts an HTTP-supplied peer-identity substitute.

The security service holds `.securityd.lock` for its lifetime. The executor holds `/var/lib/homelab-releases/.executord.lock` while serving requests. A separate release journal lock serializes staging and activation. Both Go and Rust implementations use the same owner lease, so a failed cutover cannot produce two managed writers. A service restart invalidates in-memory approvals. An unfinished audit-start record or an indeterminate result after dispatch remains unresolved and requires owner reconciliation rather than being treated as a failed or safely retriable operation.

## Source and release boundaries

`nixosModules/security.nix` and `nixosModules/executor.nix` select exactly one implementation per managed service. Go remains the default. Source completion does not authorize state migration, privileged deployment, network-edge changes, credential rotation or host generation activation. The Cargo dependency locks, negative authorization checks, state-copy preflights and isolated cutover and rollback acceptance are separate release requirements.
