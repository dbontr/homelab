# Language and service ownership policy

Homelab is one product, composed of independently operated services and optional presentation interfaces. A language is selected by its runtime responsibilities, failure modes, maintenance requirements, and integration contracts—not by the parent directory or the desire for syntactic uniformity.

## Language responsibilities

| Responsibility | Preferred implementation | Engineering rationale |
| --- | --- | --- |
| MCP, native HTTP API, external integration, concurrent controllers, orchestration and scheduling | Go | Networking, concurrency, uncomplicated deployment and established service interfaces |
| Browser console and native performance-sensitive interfaces | Rust/WASM | Strong typing, native components and portable WebAssembly targets |
| Owner-directed adaptive motivation and its private persistent state | Rust | Typed goal commitments, deterministic intervention selection and single-writer state ownership |
| Privileged execution, isolation, hardware access and narrowly scoped security mechanisms | Rust | Compile-time memory safety and precise resource ownership |
| Numerical kernels, accelerator control and FPGA/GPU-facing code | Rust; vendor C/C++ as required | Explicit memory layout, device interoperability and predictable execution |
| Relational persistence, indexing and transactional state | SQL through the owning service | Database-level constraints and transaction semantics |
| Host, service, network and cluster configuration | Nix | Reproducible builds and declarative operating state |
| Existing media and observability applications | Their supported upstream runtimes | Preserve reliable established software and its native operational model |

HTML and CSS remain the browser's presentation formats. Generated JavaScript required to bootstrap WebAssembly is not a first-party application implementation language. A new language, dependency or process must demonstrate an operational advantage before it is admitted.

## Repository source boundaries

- `services/core/` is the Go application module and owns personal planning, recipes, core coordination, the domain state store and the internal capability router.
- `services/compute/`, `services/health/` and `services/learning/` are separate Go modules with independent state and deployment boundaries.
- `services/motivation/` is an independent Rust domain process; it owns owner-approved aspirational profiles, intervention selection, in-app scheduling and privately recorded self-reports. Go forwards only its admitted operations.
- `services/bridge/` is a restricted internal transport. It owns neither domain records nor privileged credentials.
- `ui/` contains Rust/WASM presentation code and the private owner-session ingress.
- `mcp/` and `api/` contain optional Go entry points; neither can own application state, privileged execution, background schedulers or another service's credentials.
- `infrastructure/`, `nixosModules/` and `packages/` contain declarative infrastructure, release packages and deployment restrictions.

The Go module identities are part of the current source and import contracts. File-system relocation is not authority to rename module IDs or migrate persisted data.

## High-assurance Rust services

The independent security authority and privileged executor are Rust migration targets. A dependency-free Rust actuator provides an exact, second enforcement layer for systemd and Kubernetes mutations when the operator explicitly selects it. The current Go security daemon continues to own authentication, password and TOTP verification, session/confirmation state, credential custody, and independent authorization; it is never replaced by an unverified Rust implementation. Their existing Go implementations under `services/core/security/` and `services/core/privileged/` remain authoritative until the Rust services pass the same acceptance requirements. A Rust program with an untested policy is not safer merely because it uses Rust.

A replacement must preserve all of the following before it can be selected for deployment:

1. Owner password and TOTP authentication, idle and absolute session limits, immediate revocation, and operation-bound destructive confirmations.
2. Pinned mutual-TLS service identities, exact credential-grant scope, encrypted credential custody, rotation and fail-closed handling of lost authority.
3. Canonical operation and resource validation at the executor boundary, independent owner authorization, a durable pre-execution audit entry, and explicit outcome recording.
4. Exact downstream capability allowlists. Neither generic command execution nor arbitrary network proxying may be introduced.
5. Existing deployment and release contracts, persisted session and audit formats where compatible, recovery handling, and deliberate versioned migration where they are not compatible.
6. Negative tests for stale grants, conflicting credentials, malformed input, unauthorized resources, cancellation, restart and rollback.

The Go and Rust implementations must not both mutate the same durable security state. Deployment selection must have one authoritative writer, an explicit rollback path, and a release gate that rejects incomplete migration.

## Data ownership

Every application service owns its tables or records, migrations, access policy, transaction boundary and backup contract. A shared SQL server does not imply a shared schema or permission to bypass service operations. The personal state-file format must not be replaced with SQLite or PostgreSQL without a tested, rollback-safe import, field-preservation checks, and a single-writer cutover.

MCP, public API and browser sessions are clients of services. They never write another service's tables directly. Query engines and third-party provider databases remain with their owning applications.

## Release and verification

Every independently operated service must have its own build artifact, runtime identity, protected credentials, persistent data location, health signal, and rollback contract. A system-wide release may aggregate these artifacts, but deploying one interface must not replace unrelated service state.

Use the existing offline verifier and module tests for source checks. A cross-language or cross-process replacement additionally requires contract equivalence tests, authorization abuse cases, data-migration rehearsals, service shutdown/restart checks, and source-integrity validation. No component is declared production-ready merely because its source was committed or its binary compiled.

Priority belongs to reducing cross-service coupling and enforcing a single authoritative state writer. Language rewrites are justified only when their value outweighs the implementation and verification cost.
