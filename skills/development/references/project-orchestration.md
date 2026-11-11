# Project orchestration

## Canonical identity

Projects are intentional units of work and knowledge, independent of their code repositories. A project can link many repositories, libraries, artifacts, experiments, compute jobs and tasks. A library can be a standalone reusable project and serve multiple projects. Resolve those links through the authoritative service. Never conflate a project title with a repository path or materialize hidden shadow copies of personal records.

## Workflow

1. Retrieve project context, intended outcome, resources, constraints, linked repositories and current evidence.
2. Build a dependency graph of real outcomes (not a checklist of commands). Select language and environment by technical need: Go for services and orchestration, Rust/WASM for browser UI and proven performance-critical systems, Nix for fleet declarations, SQL and HTML/CSS for their native functions. Use other tools when justified.
3. Allocate isolated workspaces with explicit CPU, memory, duration, image, permitted accelerators and project authorizations. SSH inspection-only hardware must not silently become a managed worker.
4. Assign file-level write ownership if running cooperative development agents. Avoid overlapping edits and reconcile artifacts by digest and source revision.
5. Execute the smallest correct source change; preserve repository contracts, security boundaries, data owners and backward-compatibility requirements.
6. Verify with actual build/test/lint/static-analysis runs; identify what remains unexecuted. Treat workspace closure, save and publishing as independently verifiable operations.
7. Persist material decisions, exact commits, observed tests, pending work and nonsecret audit references with `continuity_checkpoint` when the connected unified MCP supports it. Preserve the current `expected_revision`; do not claim exact context-window usage without host-provided evidence. On resumption, read `continuity_handoff` and verify source state before continuing. See [conversation-continuity.md](conversation-continuity.md).
8. Submit an exact-commit draft for review; do not merge, publish, deploy or mutate live infrastructure without authorization.

## Credential ownership

Provider integration must not use ambient vault alias reads. An interactive
provider call requires the independently verified owner assertion and a
one-use ticket bound to the exact operation, target, argument object and
service certificate. Unattended workers, federated service discovery and
scheduled tasks require separately scoped service identities unless a
supported delegated authorization contract exists. Never invent an owner
assertion or retain plaintext provider credentials in a handoff.

## Autonomous and personal operations

An AI plan is a proposal, not a credential. Never promote read consent into mutation consent; owner-authenticated operations use exact targets, permissions, idempotency keys, durable audit records and service-level enforcement. Sensitive actions require independent owner authentication and destructive changes separate confirmation. The assistant can recommend priorities but must not invent completed work, learner mastery, sensor readings or the owner's emotional state.

## Repository hygiene

Keep an authoritative root `REFERENCES.md`, use canonical directories, and remove temporary artifacts, stale templates, redundant compatibility surfaces and agent-development residue. A source-development milestone can be finished yet untested; never call it production-ready without compiler, integration, security and deployment evidence.
