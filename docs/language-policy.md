# Language and service ownership policy

Homelab is one product made of independently operable services and optional interfaces. Select an implementation language according to the work performed, the guarantees required at its trust boundary, and its measured runtime characteristics. Uniformity across the repository is not a goal. Neither source-code volume nor deployment convenience is a reason to use a language that is poorly suited to a component.

The default architecture is **Go for coordination, Rust for high-assurance execution and specialized computation, SQL for transactional data, Nix for system configuration, and Rust/WebAssembly for the first-party console**. Hardware description and vendor accelerator interfaces use the languages required by those environments.

## Language selection by responsibility

| Responsibility | Preferred language | Selection criteria |
| --- | --- | --- |
| MCP server, native HTTP API, external providers, authentication-aware transport, network integration | **Go** | Concurrent I/O, protocol libraries, request lifecycle management, and established service contracts. |
| Kubernetes controllers, worker scheduling, fleet coordination, workflow orchestration, leases and background jobs | **Go** | Control-plane operations spend most of their time coordinating services and waiting for external systems. Go fits that execution model and the Kubernetes ecosystem. |
| Personal planning, recipes, health and learning service APIs, business rules and ordinary data services | **Go + SQL where appropriate** | Prefer straightforward typed domain logic, stable service contracts, and database-enforced persistence semantics. |
| Autonomous engineering orchestration, model routing, Git integration, source publication and workflow supervision | **Go** | Network-bound coordination, bounded concurrency, cancellation and auditable external operations. |
| Execution-sensitive engineering mechanisms, sandbox boundaries, memory-intensive source processing and critical state machines | **Rust** | Explicit ownership, exhaustive state handling and precise resource lifetimes where these properties strengthen correctness or performance. Do not move ordinary orchestration here solely for consistency. |
| Owner security authority, secret custody, credential broker, privileged executor and sensitive OS operations | **Rust** | A small trusted computing base, deterministic resource release, explicit memory control and strong type-enforced contracts. Use reviewed cryptographic implementations and independent authorization checks; Rust alone does not establish security. |
| Compute-worker device access, GPU/NPU/FPGA buffers, USB and other hardware-facing operations | **Rust** | Native system interfaces, explicit memory layout and controlled resource ownership. Keep Kubernetes/API-facing coordination in Go when it is primarily I/O-bound. |
| Numerical kernels, graph algorithms, simulations and other CPU-intensive computation | **Rust** | Low-level data representation and predictable native execution when profiling or algorithmic requirements justify a dedicated kernel. Retain Go for the surrounding service and SQL for its records. |
| GPU kernels and vendor accelerator integration | **Rust for host control; vendor C/C++ and CUDA/HIP where required** | Device SDKs, supported kernel compilers and hardware interfaces determine the necessary language. Do not treat host-language choice as a substitute for accelerator-native computation. |
| Synthesizable FPGA digital logic | **VHDL by default; SystemVerilog when integration requires it** | Hardware description, timing, toolchain compatibility and reusable IP. Rust or Go may control the FPGA from a processor, but neither is the default hardware-description language. |
| Browser console and reusable interactive application components | **Rust + Dioxus/WebAssembly** | Typed component state, shared Rust models and the repository's native UI architecture. WebAssembly does not inherently make DOM-bound interfaces faster; measure actual interactions. |
| Relational records, indexing, constraints, transactions and queries | **SQL through the owning service** | Let the database enforce relational invariants. Choose SQLite or another supported database based on concurrency, reliability and query needs, not on the service's implementation language. |
| Host configuration, service isolation, fleet inventory, packages, release activation and declarative networking | **Nix** | Reproducible operating state, dependency composition and explicit host/service configuration. |
| Browser structure and styling | **HTML + CSS** | Standards-based document semantics, accessibility and presentation. JavaScript generated to bootstrap WebAssembly does not authorize a separate first-party JavaScript application. |
| Small packaging, verification and operator glue scripts | **Shell** | Use for bounded composition of existing commands, not persistent services, application state, authorization or complex control flow. |
| Established third-party media, observability and infrastructure products | **Their supported upstream languages** | Preserve upstream runtime support rather than rewriting mature components to satisfy a local language preference. |

The first-party browser application uses Rust/Dioxus rather than React or TypeScript. An embedded legacy interface may continue operating until a Rust/WASM equivalent preserves its behavior and security boundary.

## Choosing between Go and Rust

Use **Go** when the component primarily calls APIs, coordinates remote work, handles requests, manages queues, schedules jobs or applies ordinary domain rules. Faster native instruction execution has limited value when the dominant costs are network latency, database operations or external compute.

Use **Rust** when a component manages privileges, secrets, device handles, raw buffers, process isolation or tight computational loops; when explicit ownership materially improves an invariant; or when representative profiling shows that allocation, garbage collection or computation warrants lower-level control.

For a mixed subsystem, split **control plane** from **execution plane** only at a real trust, lifecycle or performance boundary. A language boundary does not automatically require another daemon: choose a library, narrow process or authenticated service contract according to fault isolation and authority needs. Avoid duplicate state writers and competing implementations.

Before adopting a language for a component:

1. Identify whether its dominant work is network I/O, database I/O, native computation, hardware access, presentation or privileged execution.
2. Specify the required guarantees: authorization, memory handling, crash isolation, cancellation, determinism, latency and throughput.
3. Prefer the language in the table that meets those requirements with the least additional mechanism.
4. For a performance-driven choice, benchmark representative work and examine CPU use, peak memory, allocation rate and tail latency. Do not infer performance from language reputation alone.
5. For any security- or persistence-sensitive replacement, prove interface compatibility, data preservation, single-writer ownership and rollback before activation.

Source development may implement a preferred design without activating it. A checked-in implementation is not an operationally verified service.

## Repository ownership

- `services/core/` owns the Go platform, personal records, recipes, coordination and canonical capability dispatch.
- `services/compute/`, `services/learning/` and `services/health/` remain independent domain and persistence boundaries. Their Go controllers and APIs can call specialized Rust components without sharing unrestricted credentials or opening one another's databases.
- `services/security-authority/` implements the Rust owner security authority and its protected state and interface contracts. The Go authority remains the selected service until compilation, cross-language security tests, Nix packaging and isolated rollback acceptance pass.
- `services/bridge/` owns restricted internal transport; `mcp/` and `api/` are optional Go entry points. None owns domain state or privileged execution authority.
- `services/actuator/` provides a typed Rust privileged action mechanism. Additional trusted execution components belong to separately enforced security or execution boundaries.
- `ui/` owns Rust/Dioxus presentation code and private owner-session ingress, not domain persistence or background jobs.
- `infrastructure/`, `nixosModules/` and `packages/` own declarative host and service composition, packages and deployment controls.
- SQL schemas, migrations, access policy and transaction boundaries remain under the service that owns the records.

The Go module identities are source and import contracts. Moving a directory or choosing another implementation language does not authorize an implicit module rename or persistent-state migration.

## High-assurance security and executor boundaries

Rust is the preferred destination for the independent owner security authority and privileged executor. The existing Go security and privileged services remain authoritative until a Rust implementation passes equivalent security, state and rollback acceptance. The optional Rust actuator is an additional typed enforcement layer, not an independently authorized replacement for the owner-security service.

A replacement must preserve:

1. Owner password and TOTP authentication; 120-minute idle and 12-hour absolute session limits; immediate revocation; and exact, one-use destructive confirmations.
2. Pinned mutual-TLS service identities, exact credential grants, encrypted secret custody, rotation, revocation and fail-closed behavior when authority is lost.
3. Independent authorization at the actual executor, canonical operation and target validation, durable pre-execution audit entries, and explicit accepted-versus-completed outcomes.
4. Strict downstream operation and resource allowlists. No arbitrary shell execution, unbounded process launch or generic privileged network proxy.
5. Persisted records, supported migration formats, service shutdown/restart behavior, recovery and a tested rollback path.
6. Negative and concurrency tests for replay, stale grants, invalid credentials, corrupted audit state, unsafe paths, cancellation and conflicting writers.

Go and Rust implementations must never write the same protected state concurrently. The chosen release must have one authoritative writer and a fail-closed selection gate. Memory safety complements, but cannot replace, careful authorization, policy enforcement and auditing.

## Data ownership and migration

Each service owns its records, database credentials, schema changes, migrations, backups and access controls. MCP, API and UI processes call authenticated service operations; they do not bypass an owner to write its data. A shared database server does not grant cross-service table ownership.

The personal JSON-backed state stores remain authoritative unless an explicit database migration demonstrates complete field preservation, transaction semantics, a single-writer cutover and recovery. SQL should be preferred for relational or query-intensive data where its constraints and transactional behavior materially improve correctness; a language preference alone is not grounds for moving data.

## Verification and release

Independently operated services require distinct build artifacts, runtime identities, protected credentials, persistent-state ownership, health signals and rollback contracts. A unified release may coordinate them, but stopping or deploying the MCP or API must not control the lifetime of unrelated domain services.

Use the repository's offline verification workflow and module tests. Cross-language changes additionally require contract-equivalence tests, malformed-input and authorization cases, failure and cancellation tests, migration rehearsal where state changes, restart validation and rollback verification. Acceptance of hardware-oriented or performance-oriented components also requires tests on representative devices and workloads.

Language selection optimizes the *fitness of the component to its responsibility*. Production readiness is established by verified behavior, not by the language or the presence of source code.
