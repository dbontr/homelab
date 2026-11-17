# Project development workspaces

Homelab provides independent execution for research projects. An editor
workspace is distinct from the Kueue CPU batch queue and does not inherit
administrator access to Saturn, Kubernetes, the host filesystem, or another
project's controller data.

## Owner-authorized lifecycle

The gateway exposes `infrastructure__workspace_create` only when an
independent executor is configured. The caller supplies an exact executor
host, registered project, approved image-pool identifier, CPU, memory and
duration; the executor rechecks the host and Nix-defined allowlists,
signed owner identity, session, and operation-bound confirmation. The
controller separately checks its live worker resource and image policy.
The executor's accepted audit receipt means that creation was submitted,
not that a workspace Pod is ready.

Read `compute__workspace_list` for the project and wait for the exact
workspace identity to become `ready`. Do not infer the workspace ID from
an executor audit receipt. An approved workspace may import an exact Git
commit using `compute__git_checkout_repository`, or load a verified
project artifact using the project's existing import operation.

Use `infrastructure__workspace_command` to queue a bounded command in
that workspace. The gateway binds the caller's 32-character hexadecimal
executor `request_id` to the downstream `workspace_request_id`; the
independent Rust authority and executor enforce an exact project, workspace,
operation and argument digest before submitting it. The executor never
runs the command on the host. The workspace worker executes it inside the
existing project-isolated runtime and its resource limits.

The command's executor receipt is not proof of execution. Query
`compute__workspace_operation_by_request` using the project, workspace
and original `request_id`. This resolves the operation ID and its queued,
running, failed or succeeded state, exit code and bounded output. If the
response is ambiguous or an executor attempt is unresolved, preserve the
request identity and reconcile it; do not rerun with a new ID.

Existing read-only operation inspection, artifact storage and source
checkout remain separately authorized and project scoped. Snapshot or
checkpoint the workspace before an owner-approved stop if mutable files
must be retained. Git source, immutable project artifacts and explicit
build receipts are the durable research record.

## Compiler environments

An environment is never inferred from the assistant's local toolchain.
It is built separately, inspected and published as an immutable OCI digest.
The public gateway exposes `infrastructure__workspace_register_image`,
but registration is restricted to exact image IDs and digest-qualified image
references already present in the executor's operator policy. A publication
or catalog entry alone does not enroll a worker.

For Axiom Engine the canonical compiler recipe is in
`dbontr/axiom-engine`, `containers/Dockerfile.toolchain` on the
`feature/typed-lean-proof-pipeline` branch, with qualification requirements
in `docs/BUILD_QUALIFICATION.md`. That recipe declares Rust 1.91.1,
the `wasm32-unknown-unknown` target, Lean 4.34.0 and Lake. Its Rust base
OCI digest, elan commit, and installer checksum must be independently
supplied; none may be guessed. First qualify the lockfile, compiler
versions, Rust/WASM tests and real Lean kernel checks. Only the accepted
`offline-toolchain` image may be published without private source.

Before a release can select that image:
1. Record the source commit, exact image digest, builder identity, test
   receipts and output hashes in project knowledge and immutable artifacts.
2. Add the approved image ID and its exact OCI reference to the Nix-defined
   executor policy, scoped to `axiom-engine` or another explicitly enrolled
   project. Review alongside the Axiom project-scope changes in PR #208.
3. Add the immutable reference under
   `services.workspaceMcpProjectProvisioner.projectApprovedImages.<project>`.
   A newly enrolled worker advertises only Debian and the project-specific
   approved digests; other projects retain their original image choices.
4. The provisioner deliberately does not change an **existing** worker's
   image policy. For the four existing Axiom workers, separately stage and
   review a digest-pinned Helm worker-policy update, perform image availability
   checks on each target and verify their new capabilities. Do not restart
   active research workers or disturb hardware reservations automatically.
5. Complete owner approval, exact-source protected deployment and worker
   acceptance before creating a workspace with the new image-pool ID.

The ordinary project worker's network policy remains unchanged. Outbound
fetching of compiler packages or dependencies belongs to a separate
approved image-builder environment, not a runtime with broader privileges.
Images with missing checksums, unavailable nodes or mismatched architectures
must fail admission rather than silently switching to another compiler.

## Source-backed acceptance

In an approved Axiom workspace after source checkout, first import the
reviewed preflight script from the exact Homelab source revision as a verified
artifact, then invoke `bash verify-research-toolchains.sh 1.91.1 4.34.0`
from the imported location. The source-owned script is
`services/compute/scripts/verify-research-toolchains.sh`. Then execute the
exact commands required by the pinned
Axiom project source, including Cargo formatting, tests and Clippy, a
`wasm32-unknown-unknown` compilation, `lake build`, Lean inspector
smoke tests, and independent Lean proof acceptance/rejection tests.
Persist each terminal operation receipt and its exact source/image identities.

Successful component tests are insufficient if the source revision differs
between Rust, Lean and WebAssembly checks. The resulting proof claims are
certified only by the independent Lean checker, never by successful sample
observations or by a compiling conjecture generator.

The development capability is production-complete only when an approved
project can create a workspace, resolve its execution result across
conversations, compile all declared toolchains, retain verified artifacts,
and stop or restore the workspace without disturbing active research jobs.
