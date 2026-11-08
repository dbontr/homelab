# Workspace Project Model

## Stable project identity

A Workspace project is a durable **coordination scope** with its own ID, title, grants and optional planning/knowledge policy. Neither the title nor its repository links define its identity. A project can exist without source repositories for research administration or non-code work. When software is involved, it can link multiple repositories with distinct purposes.

Example (illustrative names, not pre-existing resources):

| Workspace relationship | Example | Authority |
| --- | --- | --- |
| Project | `numerical-research` | Workspace controller; stable ID |
| Source repository | `dbontr/solver-core` (public) | GitHub commits and permissions |
| Analysis repository | `dbontr/analysis-tools` (private) | GitHub commits and permissions |
| Project knowledge | `numerical-research` notes and contributions | Private Workspace controller storage, independent of source repos |
| Libraries | `benchmark-corpus`, `research-papers` | Workspace shared-library membership and immutable sources |
| Planning | A selected GitHub Projects V2 board | GitHub Project ID, items and fields |
| Packages | Source-linked OCI images or other GitHub Packages | GitHub package registry and package permissions |
| Artifacts | Results, datasets and binary checkpoints | Workspace content-addressed storage and verified digests |

Links are relationships, not ownership transfers. A repository may appear in several projects. One library may serve several projects. A GitHub Project can track Issues and PRs from many repositories. Do not manufacture a one-to-one project/repository mapping or clone an independent repository merely to populate a project.

## Library-focused projects and reusable publications

A **library project** is a complete Workspace project that publishes a reusable library. It can own source repositories, optional private knowledge, issues, a GitHub Projects V2 board, experiments, tests and builds, just like any other project. Its published library is a separate **consumer-facing collection** of pinned Git sources and digest-verified objects. A different project may link this library without becoming a collaborator in the publisher project.

Example: create project `lean-proofs` with `kind=library` and `library_kind=lean`. It publishes library `lean-proofs`. Publish verified proof documents with `library_document_publish`, binary artifacts with authenticated object upload, and optional source-code revisions with `library_add_source`. Projects `formal-verification` and `quantum-algorithms` independently call `library_link` for `lean-proofs` and read its published objects and pinned sources through `library_resolve`. Consumers do not acquire the publisher's private project knowledge, GitHub repository write access, or administrative grants.

The controller offers `create_library_project` (MCP) or `POST /api/projects` with `kind=library` and `library_kind=lean|research|code|mixed`. It atomically creates the Workspace project, a same-ID library publication and the publisher's own library link. `project_published_library` (or `GET /api/projects/{id}/published-library`) resolves the publication. An ordinary project is `kind=standard` even if it consumes multiple libraries.

Library-only projects do not receive automatic K3s workers unless the operator explicitly includes their project ID in the provisioner's `computeLibraryProjects` allowlist. This avoids spending four compute-worker deployments on a passive proof catalog, while allowing a proof-library project to request its own workers when compilation or formal verification warrants it. Standalone libraries without a publisher project remain valid.

A linked library currently exposes its **shared published source revision**; updating a library's source reference changes the revision seen by linked consumers. For reproducibility, resolve and save the exact Git commit in each consuming project's build, experiment or dependency manifest. Treat independent consumer-specific release pinning and publication promotions as additional features unless the live controller explicitly implements them. A GitHub release/package may convey the public library interface, but does not by itself grant access to the publisher's private knowledge.

## Project creation and linking

1. Use `list_projects`; select an existing stable project ID when it fits. Otherwise create a `standard` project or an explicitly selected `library` project only when authorized. A library project atomically publishes a same-ID reusable library. Choose an ID from the **research or work objective**, not a source repository name or the first task.
2. Use `repository_memberships` and `git_provider_capabilities` before linking. Verify each intended GitHub repository through the provider and record its numeric `id`, `full_name`, default branch, permission and role. Use `git_link_repository` or `link_github_repository` as appropriate. A loose link is valid; it need not copy source or create a checkout.
3. Assign each repo its actual role: e.g. `source`, `research`, `documentation`, `hardware`, `lean` or `planning`, according to supported schemas. Never infer access to another repository from one successful link.
4. Use `library_list` to discover reusable libraries. Create one with `library_create` or a publishing library project when authorized. Publish immutable documents and digest-verified objects locally; add pinned Git commits only when the reusable product is source code. Use `library_link` for read access and `library_resolve` for source provenance without copying records into consumer project knowledge.
5. If planning is wanted, attach an existing, explicitly selected GitHub Projects V2 board with `github_project_attach`, then inspect `github_project_board`, `github_project_fields` and `github_project_items`. Do not guess a board ID or create a board as a proxy for Workspace project creation.
6. Read `knowledge_policy` independently of repository links. The default is `local`, storing project notes and immutable contributions in Workspace. Use `none` to suspend canonical knowledge, or explicitly configure `git` when an external repository must remain authoritative. Never derive a knowledge destination from a source repository or move existing Git-backed records without verified import and readback.

## Stable repository links and rename recovery

GitHub gives repositories stable numeric IDs; `owner/name`, remote URL and display names are mutable. A preserved URL redirect is not a durable identity system: reuse of the old name can invalidate it, and some GitHub Actions references do not redirect. Record the provider ID alongside the human-readable locator in the project's private authority record when knowledge is enabled, or in an authorized operational inventory when disabled.

When a repository is renamed or transferred:

1. Read the original `repository` and previously verified numeric GitHub `id`. Identify the proposed destination. Discover permissions and any active checkout/workspace dependency.
2. Verify that the destination's GitHub repository reports the **same ID**. A matching name alone is insufficient. A new repository under an old name must not silently replace the linked identity.
3. For controller-managed changes, prefer `github_account_rename` or `github_account_transfer` with required confirmation and ID; for an external rename use `github_account_reconcile` after provider verification. Avoid force-rebinding across conflicting project memberships or active workspaces.
4. Re-read `repository_memberships`, the knowledge policy, `library_sources` and affected workspaces, and confirm each current locator resolves to the expected ID. Preserve original immutable source references and commit SHAs in historical findings; a locator change must not rewrite an experiment's provenance.
5. Update authority records and any release, package, Actions or webhook configuration that uses a mutable URL. Never assume automatic reconciliation of external changes; report the stale link as blocked until verified.

The current controller supports explicit provider-ID-verified reconciliation. Automatic external-rename detection and background link repair require separately verified runtime support. In particular, a project title change never requires renaming linked source repositories.

## Private project knowledge

**Default design:** controller-local, project-scoped notes and immutable sourced contributions. Their project access follows Workspace grants, not the visibility of linked code repositories. Shared libraries remain separate published objects with exact hashes and consumer-specific links.

Workspace authentication and project grants govern controller-local knowledge reads and writes. GitHub visibility is separate: any externally published knowledge must use a deliberately selected destination with verified permissions. A public branch, Issue, PR or planning board may disclose private content copied into it. Never publish private reasoning, context, findings, credentials or draft ideas there.

Use the local `project` note for the charter and the local `context` note for verified state and next actions. Append reproducible findings, decisions and handoffs through the immutable contribution API. Shared documents belong in publishing libraries with digest-verified content, not copied into every project. Do not create knowledge records merely because a task completed.

When migrating existing Git-backed project knowledge to Workspace local storage, verify each source file at an immutable commit, import the intended notes and contributions, read back their content and provenance, and only then change the knowledge policy. Preserve original data pending a separately authorized recovery and cleanup decision. Deleting a public Git branch cannot retract data already fetched.

## Cross-repository GitHub workflow

A Workspace project can have one GitHub Projects V2 board across multiple source repositories. Plan high-level dependencies on the board; use the relevant repository's Issues for implementation, discussions for open-ended debate when appropriate, and PRs for review of a particular repository's source. Only use matching package, release, CI, security and dependency tools when the linked repository and credential actually authorize them.

GitHub Packages (including GHCR) is a publication/distribution authority, not the project knowledge store. Treat package visibility and permissions separately; an image linked to a public repository is not necessarily public, and private repository membership does not automatically grant every runtime package access. Resolve package access explicitly before claiming a deployable environment.

## Scope and cleanup

The owner controls project names, access boundaries and knowledge settings. The controller owns project knowledge, operational state and leases; GitHub owns selected repositories and planning; libraries own reusable content references; artifacts own verified binary bytes. Do not mirror an authoritative issue backlog as a Markdown task list or duplicate package binaries in knowledge Git. Do not create a project merely to retain conversation context. A valid output may be a code change, artifact, issue/PR, library object, experiment record or no durable record at all.
