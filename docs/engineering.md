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
7. Export the verified files as immutable project artifacts. The service compares each export with the final verified file digest and checks the controller's stored artifact metadata before publication.
8. Prepare a revision-guarded **draft pull request**. A run cannot merge it or activate a release.

The engineering service authenticates with a project-scoped compute credential, not the compute controller administrator token. The controller requires an editor grant on the selected project before accepting a draft publication, and the repository must be linked to that project. The daemon checks its credential identity at startup and rechecks each operation when credentials are loaded from a protected file; rotating to an administrator credential is rejected.

The program's `allowed_paths` restricts model-generated output. In fixed-plan runs, `context_paths` selects files initially supplied to the model. Autonomous runs can explore additional repository files through bounded inspection tools, subject to protected-path restrictions and compute-worker isolation. Neither grants access to credentials, host files, the release controller or privileged cluster commands. Source-code and test-output inputs are untrusted and can contain adversarial instructions. The authoring client accepts only a bounded JSON file map and rejects paths outside the allowlist.

Execution uses the compute controller's authenticated MCP service, but the engineering daemon has its own narrowly scoped client with a fixed operation allowlist. The gateway uses a Unix-domain-socket client to submit, inspect and cancel runs; it never receives compute or model credentials. The engineering daemon maintains private durable state, lease-token digests, fenced generations and controlled cancellation. An ambiguous external result is not assumed to have failed; the runner does not repeat an external step beyond its single allowed attempt. The compute workspace expires according to its allocated duration, and operation history remains available for inspection.

## Autonomous agent sessions

An engineering program can set `"autonomous": true` to execute a model-directed, bounded development session. This mode requires an operator-configured decision model, an explicit list of `allowed_paths`, and no pre-supplied `changes`. The project, repository, image, exact commit, resource limits, and owner-provided `checks` remain mandatory. Ordinary fixed-plan runs remain available.

The compiled autonomous workflow provisions the same isolated compute workspace and checks out the same pinned revision, but replaces the fixed inspect/author/apply stages with durable agent turns. Each turn selects one operation from a restricted vocabulary:

- `plan`: record or revise up to eight development steps.
- `list`: inspect a repository-relative directory.
- `search`: perform a bounded literal search inside the checked-out repository; common credential files and dependency trees are excluded.
- `map`: enumerate up to 24 source files in the selected repository directory, with bounded declaration outlines and import lists. Go declarations use the standard parser; Rust, Python, JavaScript/TypeScript, Java, C/C++, and VHDL use conservative lexical extraction.
- `symbols`: extract declaration names, kinds, line numbers, and imports from an individual supported source file without returning the entire file body.
- `references`: search for bounded whole-identifier matches in source files under the chosen repository directory. The search excludes dependency folders and hidden configuration directories.
- `read`: inspect a repository-relative source file and its SHA-256 revision. Credential-bearing paths are excluded from reading and editing, even if they appear in an owner's `allowed_paths`.
- `patch`: apply ordered, exact-match replacement hunks to an allowed file only when its SHA-256 matches the model's expectation. Ambiguous context and stale revisions are rejected.
- `create`: create an owner-approved source file through a missing-file revision check.
- `check`: execute one command exactly matching the owner's `checks` list in the isolated workspace.
- `finish`: record a short completion summary only after at least one confirmed edit and every approved check has passed since the most recent edit.

Repository maps are bounded by file count, file size, and observation size. They are navigation aids rather than authoritative compiler dependency graphs. Repository content remains untrusted, and map results never grant permission to edit any file outside the declared `allowed_paths`.

The agent receives its objective, current plan, a bounded suffix of past observations, changed-file digests, and remaining decision budget. It can revise its plan or inspect more code when observations contradict its earlier assumptions. Each action decision is persisted before execution and its confirmed observation is saved before the next decision. The workflow permits at most 40 turns. The authenticated status API exposes the plan, turn count, edited file paths, and completion summary but does not return raw source code or model observations.

Checkpoint updates are fenced by the active run, step, lease generation, and lease token. An interrupted write can be reconciled against the worker's current file digest without blindly repeating it; a write is confirmed only after a worker SHA-256 acknowledgment and source readback. An interrupted verification command is not replayed because its effects may be uncertain. Unapproved commands, out-of-scope edits, or attempts to access protected paths terminate the agent step.

The engineering scheduler advances up to four independent runs concurrently. Each run remains sequential and dependency-ordered, with its own workspace, lease, and cancellation context. Project quotas and resource admission remain enforced by the compute controller; simultaneous publication attempts must still satisfy the pinned Git base revision.

All approved checks are run again after the agent finishes. Before exporting, the service compares the current source digests with the files confirmed by the agent. Publication remains restricted to a pinned-base **draft** pull request; autonomous sessions cannot merge, deploy, manage infrastructure, or acquire an administrator credential. The decision model and compute worker are separate trust boundaries.

### Verified source and artifact identity

For fixed-plan jobs, verification records the SHA-256 digest of every authored file after the final passing check. Bounded repairs update those digests. A check that unexpectedly alters an authored file fails verification; the engine never substitutes the original generated bytes for a repaired file.

Immediately before export, the engine checks the workspace file against the verified digest. Export responses may report either a top-level `artifact_id` or nested `artifact.id` metadata. In either case, the service obtains `artifact_info` from the authenticated compute controller and verifies the artifact ID, project ownership, and stored SHA-256 digest. An absent or mismatched artifact blocks draft publication.

## Engineering compute leases and source identity

The engineering program can declare `hardware` as a bounded map of `gpu` or `npu` to device counts, with at most two accelerators in a run. `min_gpu_memory_mb` requires a GPU allocation. `node_id` and `architecture` optionally select the exact approved worker and machine architecture. CPU quantities must be finite. These declarations are forwarded unchanged to the Compute controller during `workspace_create`, where image approval, resource capacity, device ownership and lease conflicts are checked atomically. The engineering daemon does not create hardware leases itself and does not grant access to external boards. The Rust console accepts these options in its engineering run form.

Every engineering run records an immutable `inputs` manifest with the complete request SHA-256, pinned primary and read-only repository revisions, selected compute image and hardware constraints, exact verification commands and permitted edit paths. Supplied file contents are represented by SHA-256 fingerprints, not copied into the public run summary. The verified export step separately records artifact IDs after source readback and controller SHA-256 verification. Together these provide inspectable provenance; they do not imply reproducible machine output unless independent builds yield matching artifact hashes.

## Configuration

The NixOS service is `services.homelabEngineering`. Its module is exported from the root flake. It is disabled by default and needs an exact compute MCP endpoint, a service-scoped compute token file, and a finite list of permitted project IDs. Source-model integration is optional; without it, runs must contain complete owner-supplied `changes`.

The model client accepts a configured `/v1/chat/completions` endpoint over HTTPS or loopback HTTP. An optional bearer credential is loaded by the independent daemon from a protected file. The model adapter is deliberately separate from the gateway's authorization and the privileged executor.

The platform's `services.homelabPlatform.engineeringSocket` may point to `/run/homelab-engineeringd/engineering.sock` when both modules are enabled. The gateway is placed in the local engineering-client group, not in the daemon's credential store. The engineering service makes no production infrastructure changes merely by being enabled.

The native endpoints are:

- `GET/POST /v1/engineering/runs`
- `GET /v1/engineering/runs/{id}`
- `POST /v1/engineering/runs/{id}/cancel` with the exact run revision

The MCP tools `engineering_run_submit`, `engineering_run_get`, `engineering_run_list` and `engineering_run_cancel` call the same implementation. The Rust console displays run progress and links to any confirmed draft pull request.


## Multi-repository projects

An autonomous engineering program has one primary Git repository and an optional
`linked_repositories` array. Each linked repository has a full provider name and
an immutable commit SHA. The controller must already authorize the repository
for the selected project. The runner checks out the pinned sources into separate
directories in the same isolated environment. A model action may select a linked
repository for read-only `list`, `search`, `map`, `symbols`,
`references`, and `read` operations. Edits, verification commands, artifact
export, and draft publication remain confined to the primary source repository.
No action can turn a reference repository into an editable one.

## Project knowledge and shared libraries

The `include_knowledge` option permits the engineering model to request
canonical project context, immutable decisions, source-linked research passages,
and linked library metadata through the existing compute controller.
The controller's project knowledge policy and library grants remain
authoritative. These actions are read-only, use bounded responses, and do not
create a separate knowledge database. Disabled knowledge remains unavailable
to the agent. The console shows the project's own source links and indexed
research independently of engineering-run execution.

## Cooperative engineering

An engineering team consists of two to four explicitly scoped autonomous
members. The owner supplies a common pinned project/repository configuration
and a specific objective, role, identifier, and file allowlist for each member.
A source path may belong to only one member. All members run in separate
resource-limited workspaces. Their durable workflow states are associated
under a single team record, and cancellation can fence unfinished members.

Members prepare independently verified artifacts but do not publish their
own pull requests. The coordinator constructs a deterministic artifact
manifest from completed member workflows and checks each output against its
member's allowed paths. Duplicate or unauthorized paths are reported as
reconciliation conflicts. A group is ready for owner review only after every member has completed
and its artifacts are conflict-free. A separate owner-authorized integration
workflow imports the verified artifacts into a fresh checkout, runs the
combined checks, and prepares a single reviewable draft. No member may merge
or deploy changes, and conflicting artifacts are never resolved implicitly. Teams are available through the engineering
daemon's `/teams` lifecycle interface, the authenticated platform API
`/v1/engineering/teams`, and the `engineering_team_*` MCP tools.

## Code manipulation and review

The autonomous model may preview an exact-context patch without writing it.
The `format` action accepts an approved Go file and a matching source digest,
uses the standard Go parser/formatter, and commits the formatted contents
through the worker's revision-checked write path. Other languages retain the
existing exact-context patch mechanism; language servers can be used in
approved test environments without introducing an independent editor.

The optional `review` gate runs after all owner-defined verification checks.
The reviewer examines bounded, SHA-256-verified final source and recorded check
metadata, then returns a structured verdict, risk classification, findings,
and recommended tests. A request to revise, or an inconsistent high-risk
approval, stops draft publication. The review is evidence-informed analysis,
not a formal proof of correctness. The existing independent human-review and
deployment authorizations remain mandatory.

## Console

The native Rust/WASM console provides authenticated engineering-run creation,
execution progress, planning steps, changed-file paths, verification results,
review findings, and links to confirmed GitHub draft diffs. The cooperative
engineering view displays member roles and run status. Project Knowledge
shows authorized project context, repository links, shared libraries, and
source-linked research searches. Native endpoints continue to enforce the
same owner-session authentication and CSRF controls as other applications.


## Autonomous task allocation

Cooperative engineering accepts either owner-specified tasks or an explicitly
requested `auto_plan`. The planning model may assign two to four tasks, but
cannot expand the owner's approved file list, repository pins, checks, project
authorization, or resource limits. The engine checks the proposed roles,
objectives, nonoverlapping source paths, and acyclic `depends_on` graph before
committing workflows. Dependencies are durable and the scheduler admits each
member only after all predecessor runs have succeeded. Scheduling interleaves
projects and limits concurrent steps to four overall and two per project.

## Integrated publication

A cooperative team can schedule one separate integration workflow once all
members have verified and exported authorized artifacts. The coordinator
checks project ownership, exact source SHA-256 and file ownership, then
allocates an isolated workspace and checks out the common pinned base commit.
It imports every artifact into the source tree and verifies each readback
digest. It then runs the original owner's approved checks against the
**combined** files, and verifies again that those files did not change.

Only after combined verification can the controller prepare a single draft
pull request against the exact expected Git base revision. External
publication intent is durably recorded before the GitHub call. An interrupted
or uncertain response cannot be retried without owner reconciliation. A
team's integration run must be linked from its durable team record before
any of its steps can be claimed. Neither agents nor the coordinator can merge
or deploy. Integration approval requires an authenticated owner request.

## Durable engineering knowledge

When project knowledge is enabled, an autonomous agent may propose a decision,
finding, or note supported by one to three readable source files. The service
records source path hashes, the pinned repository commit, a content-derived
proposal identity and a bounded body in private workflow state. Repeated
proposals with the same provenance and content are deduplicated.

Proposals are not written automatically to canonical project knowledge.
The owner can inspect the complete proposed text and source evidence through
the authenticated gateway. Approval is permitted only for a successful run.
The approval state is durably marked before the controller's
`knowledge_contribute` call, and an uncertain contribution requires
reconciliation instead of blind replay. Existing project knowledge policies
and repository grants remain authoritative.

## Conservative test-impact analysis

The read-only `test_impact` engineering action accepts a single validated source path in the pinned primary or linked repository. It inspects the existing bounded repository map and declared imports, suggests test files in the same directory or source packages dependent on that directory, and explicitly marks incomplete source maps. This is heuristic test selection: a reported candidate is not proof of relevance, and missing candidates do not imply that a test suite can be skipped. The action cannot execute checks or expand the owner-approved test command list.

## Source navigation and diagnostics

The `dependency_graph` action returns a bounded graph of declared imports
and source files, not a compiler-certified dependency graph. A read-only
`lsp` action requests Go definition, reference or hover information from
`gopls` installed inside the isolated workspace. The action is restricted to
pinned source files and positional arguments, with Go module network
resolution disabled. If `gopls` is not installed, the agent can continue
with the existing AST-based outlines and source search.

Every model-selected approved verification command records a bounded
diagnostic category, exit code, SHA-256 of its output, and parsed coverage
percentage when present. These records help the autonomous planner diagnose
failures, change only allowed code or regression tests, and rerun the same
owner-approved checks. The model may suggest tests, but cannot invent shell
commands outside the existing program authorization.

## Execution budgets and workspace recovery

Every compiled workflow carries an absolute execution deadline derived from
its approved resource duration. Running tasks use a deadline-bounded context;
an overdue workflow is cancelled rather than silently extended. The
scheduler never admits an integration run that is not linked to its owning
team.

After a run succeeds, fails or is cancelled, a separate reconciler inspects
the real worker state, requests workspace teardown, and confirms that the
workspace stopped or disappeared. Cleanup status and attempts survive daemon
restarts. Attempts are bounded, and an uncertain state is visible to the
owner instead of being reported as successful cleanup.

## Engineering console

The Rust/WASM engineering console presents owner-authorized task planning,
a selectable agent dependency graph, the integrated artifact manifest, final
verification and cleanup status, source review findings, and links to the
reviewable GitHub draft diff. It displays bounded agent activity and check
diagnostics. Knowledge proposals require the owner to inspect their contents
and provenance before approving contribution to project knowledge.

The project-scoped acceptance command and credential prerequisites are documented in [Isolated engineering acceptance](engineering-acceptance.md). The CLI operates without deploying or activating an engineering daemon.

## Verification and operational constraints

Source presence is not evidence of a tested runtime. Before enabling the daemon, build the Go service, run the behavioral and negative-authorization tests, exercise a linked test repository through the actual compute worker, verify artifacts and Git publication, and confirm project/credential isolation.

A completed run establishes only that its requested checks returned success and the draft publication was confirmed. It does not establish full functional correctness, test coverage or production readiness. The owner must review the draft before merge; releases continue through the independent operator-authorized release process.

Compute credentials must be project-scoped, and GitHub permissions must not be broader than the repositories permitted to the engineering service. Automated execution, source edits and draft publication can have material cost and security effects. A development environment should permit outbound network access only as explicitly required by the job.

## Local state ceiling

The local workflow store admits at most 1,000 run records. This prevents unbounded linear scans and response growth while the service has no archival workflow. Before this ceiling is reached, implement explicit owner-authorized terminal-run archival with revision checks and auditable retention policy. Do not delete active or uncertain runs automatically.

The isolated Go package and engineering daemon can be built and tested without a live fleet. Running the test suite with race detection validates local state and simulated backend contracts, not the compatibility of the installed controller, Kubernetes workers, GitHub credentials, or console. Runtime acceptance must establish those separately.
