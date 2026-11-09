# Engineering execution

Engineering is a capability of Homelab. The owner uses the same authenticated native API and MCP identity as the rest of the platform. Its compute controller, source authoring, release executor and credential broker are internal trust boundaries, not separate user-facing products.

## Execution contract

An engineering run belongs to one explicitly permitted project, independently of how many repositories it links. The requested repository must be linked to that project in the compute controller. The run pins one exact 40-character Git commit. Publication requires the same expected head and a named base branch.

Runs are compiled into these dependency-ordered operations:

1. Provision a CPU/memory/time-limited compute workspace from an approved image.
2. Import an exact repository revision into that workspace.
3. Read only the specified source context files.
4. Obtain a bounded set of complete source files, either from the owner or an operator-configured source model.
5. Apply edits only to approved repository-relative paths.
6. Execute the declared verification commands in the isolated workspace and record confirmed exit codes. If a check fails and a source model is configured, permit up to two bounded repairs of files already modified and explicitly included in the owner's `allowed_paths`. Repeat all checks after each repair; any unresolved failure blocks publication. Only the failing command and exit code, not raw test logs, are included in model repair feedback.
7. Export the verified files as immutable project artifacts.
8. Prepare a revision-guarded **draft pull request**. A run cannot merge it or activate a release.

The engineering service authenticates with a project-scoped compute credential, not the compute controller administrator token. The controller requires an editor grant on the selected project before accepting a draft publication, and the repository must be linked to that project. The daemon checks its credential identity at startup and rechecks each operation when credentials are loaded from a protected file; rotating to an administrator credential is rejected.\n\nThe program's `allowed_paths` restricts model-generated output. `context_paths` restricts which existing files are presented to the model. Neither grants access to credentials, host files, the release controller or privileged cluster commands. Source-code and test-output inputs are untrusted and can contain adversarial instructions. The authoring client accepts only a bounded JSON file map and rejects paths outside the allowlist.

Execution uses the compute controller's authenticated MCP service, but the engineering daemon has its own narrowly scoped client with a fixed operation allowlist. The gateway uses a Unix-domain-socket client to submit, inspect and cancel runs; it never receives compute or model credentials. The engineering daemon maintains private durable state, lease-token digests, fenced generations and controlled cancellation. An ambiguous external result is not assumed to have failed; the runner does not repeat an external step beyond its single allowed attempt. The compute workspace expires according to its allocated duration, and operation history remains available for inspection.

## Autonomous agent sessions

An engineering program can set `"autonomous": true` to execute a model-directed, bounded development session. This mode requires an operator-configured decision model, an explicit list of `allowed_paths`, and no pre-supplied `changes`. The project, repository, image, exact commit, resource limits, and owner-provided `checks` remain mandatory. Ordinary fixed-plan runs remain available.

The compiled autonomous workflow provisions the same isolated compute workspace and checks out the same pinned revision, but replaces the fixed inspect/author/apply stages with durable agent turns. Each turn selects one operation from a restricted vocabulary:

- `plan`: record or revise up to eight development steps.
- `list`: inspect a repository-relative directory.
- `search`: perform a bounded literal search inside the checked-out repository; common credential files and dependency trees are excluded.
- `read`: inspect a repository-relative source file and its SHA-256 revision. Credential-bearing paths are excluded.
- `patch`: apply ordered, exact-match replacement hunks to an allowed file only when its SHA-256 matches the model's expectation. Ambiguous context and stale revisions are rejected.
- `create`: create an owner-approved source file through a missing-file revision check.
- `check`: execute one command exactly matching the owner's `checks` list in the isolated workspace.
- `finish`: record a short completion summary only after at least one confirmed edit and every approved check has passed since the most recent edit.

The agent receives its objective, current plan, a bounded suffix of past observations, changed-file digests, and remaining decision budget. It can revise its plan or inspect more code when observations contradict its earlier assumptions. Each action decision is persisted before execution and its confirmed observation is saved before the next decision. The workflow permits at most 40 turns. The authenticated status API exposes the plan, turn count, edited file paths, and completion summary but does not return raw source code or model observations.

Checkpoint updates are fenced by the active run, step, lease generation, and lease token. An interrupted write can be reconciled against the worker's current file digest without blindly repeating it; a write is confirmed only after a worker SHA-256 acknowledgment and source readback. An interrupted verification command is not replayed because its effects may be uncertain. Unapproved commands, out-of-scope edits, or attempts to access protected paths terminate the agent step.

All approved checks are run again after the agent finishes. Before exporting, the service compares the current source digests with the files confirmed by the agent. Publication remains restricted to a pinned-base **draft** pull request; autonomous sessions cannot merge, deploy, manage infrastructure, or acquire an administrator credential. The decision model and compute worker are separate trust boundaries.

## Configuration

The NixOS service is `services.homelabEngineering`. Its module is exported from the root flake. It is disabled by default and needs an exact compute MCP endpoint, a service-scoped compute token file, and a finite list of permitted project IDs. Source-model integration is optional; without it, runs must contain complete owner-supplied `changes`.

The model client accepts a configured `/v1/chat/completions` endpoint over HTTPS or loopback HTTP. An optional bearer credential is loaded by the independent daemon from a protected file. The model adapter is deliberately separate from the gateway's authorization and the privileged executor.

The platform's `services.homelabPlatform.engineeringSocket` may point to `/run/homelab-engineeringd/engineering.sock` when both modules are enabled. The gateway is placed in the local engineering-client group, not in the daemon's credential store. The engineering service makes no production infrastructure changes merely by being enabled.

The native endpoints are:

- `GET/POST /v1/engineering/runs`
- `GET /v1/engineering/runs/{id}`
- `POST /v1/engineering/runs/{id}/cancel` with the exact run revision

The MCP tools `engineering_run_submit`, `engineering_run_get`, `engineering_run_list` and `engineering_run_cancel` call the same implementation. The Rust console displays run progress and links to any confirmed draft pull request.

## Verification and operational constraints

Source presence is not evidence of a tested runtime. Before enabling the daemon, build the Go service, run the behavioral and negative-authorization tests, exercise a linked test repository through the actual compute worker, verify artifacts and Git publication, and confirm project/credential isolation.

A completed run establishes only that its requested checks returned success and the draft publication was confirmed. It does not establish full functional correctness, test coverage or production readiness. The owner must review the draft before merge; releases continue through the independent operator-authorized release process.

Compute credentials must be project-scoped, and GitHub permissions must not be broader than the repositories permitted to the engineering service. Automated execution, source edits and draft publication can have material cost and security effects. A development environment should permit outbound network access only as explicitly required by the job.

## Local state ceiling

The local workflow store admits at most 1,000 run records. This prevents unbounded linear scans and response growth while the service has no archival workflow. Before this ceiling is reached, implement explicit owner-authorized terminal-run archival with revision checks and auditable retention policy. Do not delete active or uncertain runs automatically.

The isolated Go package and engineering daemon can be built and tested without a live fleet. Running the test suite with race detection validates local state and simulated backend contracts, not the compatibility of the installed controller, Kubernetes workers, GitHub credentials, or console. Runtime acceptance must establish those separately.
