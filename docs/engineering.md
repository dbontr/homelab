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
6. Execute the declared verification commands in the isolated workspace and record confirmed exit codes.
7. Export the verified files as immutable project artifacts.
8. Prepare a revision-guarded **draft pull request**. A run cannot merge it or activate a release.

The program's `allowed_paths` restricts model-generated output. `context_paths` restricts which existing files are presented to the model. Neither grants access to credentials, host files, the release controller or privileged cluster commands. Source-code and test-output inputs are untrusted and can contain adversarial instructions. The authoring client accepts only a bounded JSON file map and rejects paths outside the allowlist.

Execution uses the compute controller's authenticated MCP service, but the engineering daemon has its own narrowly scoped client with a fixed operation allowlist. The gateway uses a Unix-domain-socket client to submit, inspect and cancel runs; it never receives compute or model credentials. The engineering daemon maintains private durable state, lease-token digests, fenced generations and controlled cancellation. An ambiguous external result is not assumed to have failed; the runner does not repeat an external step beyond its single allowed attempt. The compute workspace expires according to its allocated duration, and operation history remains available for inspection.

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
