# Language and service ownership policy

Homelab is a self-hosted research and personal computing platform composed of independently operable services. The primary application language is **Go**. The first-party browser interface uses **TypeScript, React, shadcn/ui, and Tailwind CSS**. Nix remains the declarative host and release language; SQL owns transactional data constraints.

Language uniformity must not compromise an established hardware interface, an independently specified source format, or a vendor compiler. Existing deployed services remain authoritative during a verified migration. No source change alone authorizes a protected production cutover.

## Responsibility and language

| Responsibility | Source language | Owner and guarantees |
| --- | --- | --- |
| MCP, HTTP APIs, OAuth integration, adapters and orchestration | Go | `services/core`, single authenticated API boundary |
| Independent owner security authority, passwords, TOTP, vault and credential grants | Go | `services/core/security`, single protected state owner |
| Privileged executor, operation policy, audit, release and hardware actions | Go | `services/core/privileged`, independent signed authorization and durable request receipts |
| Kubernetes Compute, scheduling, hardware leases and worker coordination | Go | `services/compute`; keep existing jobs and project data stable |
| Engineering runs, workspace creation, source checkout, checks and publication | Go | `services/core/agentflow`, independent project editor identity |
| Learning, Health, recipes, planning, monitoring, motivation and ordinary background services | Go | Separate domain ownership and private persistence |
| Native device access and constrained build execution | Go using typed OS interfaces and reviewed SDK bindings | Keep explicit privileges and resource limits; don't invoke an arbitrary shell |
| Browser console and reusable interaction components | React + TypeScript + shadcn/ui | `ui/web` static assets served behind the Go API and Cloudflare Access |
| Transactional records and indexes | SQL via owning service | Enforce relational invariants and ownership in the database |
| Host, cluster and release configuration | Nix | Pinned dependencies, isolated services and protected exact-source activation |
| FPGA hardware description | VHDL, with vendor-supported languages as necessary | Synthesis and timing tools determine the source language |
| GPU kernels and vendor-specific programming | Supported native SDK languages where required | Keep the host-facing integration in Go; do not pretend Go can replace kernel toolchains |

Go is not inherently a substitute for memory safety, authorization, or cryptographic correctness. Privilege is a property of process identity, operating-system policy, validated arguments, and independent authorization—not of the programming language. Do not collapse Security, Executor, and general platform operations into one unrestricted process simply because they share Go libraries.

## Security authority and executor

The Go owner security service retains password and TOTP verification, 120-minute idle and 12-hour absolute owner-session limits, session revocation, single-use destructive confirmations, bound caller identity, pinned issuer and audience, independent credential custody and AES-GCM vault protection. The protected executor retains exact operation and resource allowlists, canonical argument verification, immutable program paths, bounded execution, persistent audit, duplicate-request fencing and uncertain-outcome reconciliation.

Both are separate services with independent credentials, state directories, runtime identities and sockets. The MCP and browser are clients; neither owns privileged state nor an administrator token. Never introduce a generic shell command or unrestricted cluster API in place of typed operations.

Authority migration requires the existing protected state-version and parent-bound receipt protocol. The Go security preflight must validate an isolated copy of the existing replay and encrypted vault records before the service selection changes. The old and new services must never hold the same state writer lease simultaneously; a missing or mismatched Go selection receipt must deny startup. Owner approval is completed through the independent portal, not ChatGPT, the web console, or NanoKVM.

Executor migration requires the Go policy, authorization client, typed actuator, audit journal, replay and recovery tests to pass with the actual operator configuration. A Go release binary must not quietly fall back to the old actuator when the Go policy is incomplete. Release activation remains separately authorized, with an exact source commit, staged release identity, health checks and rollback.

## Browser interface

The browser is a client of the existing Go-owned same-origin endpoints. Use pinned React, TypeScript and shadcn/ui packages with a committed npm lockfile and an immutable Nix build. shadcn/ui components are source-owned, MIT-licensed building blocks, not a remotely executed UI dependency. Preserve upstream notices and accessibility requirements.

Every browser request is independently subject to signed Cloudflare Access JWT validation at the Go gateway. Browser mutations require an assertion-bound CSRF token, exact allowed Origin, allowed content type and resource-specific policy. No public UI route can call privileged infrastructure operations, read owner secrets, or bypass owner confirmation.

Do not introduce new client-side databases for canonical Projects, Compute or personal records. Loading, missing, permission-denied, and stale states must be explicit. Do not synthesize health, worker readiness or progress percentages.

## Migration and operational acceptance

An isolated source branch may implement the Go and TypeScript architecture while production continues to run the existing services. This coexistence is temporary and must not imply simultaneous state writers. Remove superseded implementations only when their behavior and persisted records have been verified under the adopted implementation and rollback is available.

Required acceptance covers:

1. Go Core, Compute, Security and Executor unit, race, malformed-input, negative-authorization and replay suites.
2. Offline credential and owner-session state migration, vault authentication and parent-bound selection receipts.
3. Privileged executor service, Kubernetes, workspace and exact-source NixOS release tests, including lost responses and restart behavior.
4. React TypeScript strict typecheck, dependency lock validation, production static build and browser accessibility tests.
5. Contract parity against the existing Go browser routes, including all established project, Compute, Engineering, Health and personal user flows.
6. An isolated integration candidate and independently authorized production activation with safe rollback; no research job or project data may be interrupted without explicit approval.

Go and TypeScript reduce development complexity only when the delivery workflow is simpler. The correct completion criterion is that an authenticated project can provision an approved workspace, build its exact source revision, retrieve durable results, and use the web console without repeated infrastructure development.
