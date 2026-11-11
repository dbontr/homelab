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
`imageReferences`, `software`, `programs`, `packages`, `workers`,
`devices` and `projects` supply independent resource allowlists. All seven
workspace families require exact operation-bound owner authorization,
and catalog writes plus workspace creation require one-use confirmation.
The gateway neither reads the controller's privileged token nor forwards
an arbitrary privileged MCP tool to the controller. A release must verify
both Go and Rust policy implementations against the same deny fixtures.

Every mutation uses a canonical JSON object. The security authority binds authorization to the operation, target, arguments, authenticated owner and current session. Destructive confirmations are bound to one exact canonical digest and cannot be reused. A durable audit-start event precedes each external side effect. A completed command is not sufficient evidence that an asynchronous downstream job has completed.

## Socket and state ownership

The security service owns `/run/homelab-securityd/client.sock` and the inaccessible-to-gateway owner socket. The executor owns `/run/homelab-executord/execute.sock`; the platform joins only its constrained client group when explicitly configured. Optional remote authorization and release listeners use TLS 1.3, configured trust roots and exact certificate DNS identities. No listener accepts an HTTP-supplied peer-identity substitute.

The security listener separates authorizing clients (`remote.allowedClientDNS`) from credential-only TLS identities (`remote.credentialClientDNS`). The lists are disjoint and contain no more than sixteen canonical DNS identities in total. Only authorizing clients can request `/v1/authorize` or `/v1/preview`. A credential grant must name an authorizer identity and an exact permitted operation and target; an optional canonical-arguments digest narrows it further. After a consuming owner authorization, the authority returns a one-use, 60-second ticket only to that verified executor. The executor redeems it through `POST /v1/service-credentials/{alias}` with the original canonical request. The authority binds the ticket to the owner session, certificate identity, operation, target, argument digest and credential revision, and consumes it before returning any secret bytes. Credential-only identities cannot mint tickets or read credentials from an alias alone. Neither role can change its identity through an HTTP header.

The security service holds `.securityd.lock` for its lifetime. The executor holds `/var/lib/homelab-releases/.executord.lock` while serving requests. A separate release journal lock serializes staging and activation. Both Go and Rust implementations use the same owner lease, so a failed cutover cannot produce two managed writers. A service restart invalidates in-memory approvals. An unfinished audit-start record or an indeterminate result after dispatch remains unresolved and requires owner reconciliation rather than being treated as a failed or safely retriable operation.

## Security state migration

The Rust security daemon requires an existing, protected `authenticator.json` replay record. Missing or unrecognized authentication state, invalid vault authentication, or missing vault keys must not be treated as fresh enrollment during an implementation switch.

Before cutover, inspect an independent offline copy with `security-preflight --state-dir ABSOLUTE_COPY_DIRECTORY` and the intended daemon's protected credential and policy environment. The directory must be owner-only (0700). Copied state files must be owner-only regular files (0600 or stricter), have one hard link, and have inodes distinct from live state. File or directory bind mounts of live state are not acceptable. Inspection creates no state files and acquires no writer lease.

The Go and Rust daemons share `.securityd.lock`, which allows only one process to own the state. Only an authorized NixOS generation switch may select the Rust daemon. Stop the active owner and verify lease release before cutover. Keep the preceding generation and protected state available for rollback; neither sessions nor destructive confirmations survive a restart.

## Unresolved execution reconciliation

The Go and Rust executors share the same owner-only `executor-actions.jsonl`
journal and `.executord.lock`. An attempt that remains `started`, terminates as `indeterminate`, or has
an older Go `failed` receipt may have applied its side effect. Both runtimes
reject another `started` event with the same operation, target, and canonical
argument digest until the attempt has been reconciled. An independent request
with a different digest is not automatically denied. The journal also rejects
duplicate audit IDs and contradictory terminal transitions.

Only a trusted root console may reconcile an attempt. Stop the active Go or
Rust executor and verify that it released its process-lifetime state lease.
Inspect the relevant host objects through an independent local channel.
Never mark an operation `not_applied` merely because the caller timed out or
a status response was lost. If the result remains uncertain, keep the attempt
unresolved and do not retry it.

On either Go or Rust executor hosts, use
`homelab-audit-inspect unresolved` to inspect outstanding attempts. The
NixOS command selects the matching implementation and pins the protected
journal directory instead of trusting a caller-supplied location.
Prepare an owner-only, root-owned JSON evidence file with these exact fields:
`attempt_id`, `operation`, `target`, `arguments_sha256`, `finding`,
`verified_by`, `inspected_at`, and `explanation`. The finding must be
`applied` or `not_applied`; ambiguous outcomes cannot be reconciled.
Use the inspected attempt's exact identity, operation, target, and digest, an
RFC 3339 inspection timestamp, and an explanation of the independently
verified state. The explanation must contain at least 32 characters.
The evidence file must have mode 0600 or stricter and belong to root.

After separately retaining the evidence file in protected operator custody,
invoke `homelab-audit-inspect reconcile AUDIT_ID EVIDENCE_FILE --confirm`.
This appends an immutable `reconciled` event to the audit journal, containing
the SHA-256 digest of the supporting evidence rather than plaintext notes.
The event does not assert that a prior mutation succeeded, cannot rewrite an
earlier receipt, and does not itself execute or retry anything. The Go
executor accepts the same event format during rollback to Go. If the audit
file fails validation or its inode changes, neither runtime may perform
privileged effects until independent recovery.

## Source and release boundaries

`nixosModules/security.nix` and `nixosModules/executor.nix` select exactly one implementation per managed service. Go remains the default. Source completion does not authorize state migration, privileged deployment, network-edge changes, credential rotation or host generation activation. The Cargo dependency locks, negative authorization checks, state-copy preflights and isolated cutover and rollback acceptance are separate release requirements.
