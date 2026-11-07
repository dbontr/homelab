# Workspace Project Model

## Stable project identity

A Workspace project is a durable **coordination scope** with its own ID, title, grants and optional planning/knowledge policy. Neither the title nor its repository links define its identity. A project can exist without source repositories for research administration or non-code work. When software is involved, it can link multiple repositories with distinct purposes.

Example (illustrative names, not pre-existing resources):

| Workspace relationship | Example | Authority |
| --- | --- | --- |
| Project | `numerical-research` | Workspace controller; stable ID |
| Source repository | `dbontr/solver-core` (public) | GitHub commits and permissions |
| Analysis repository | `dbontr/analysis-tools` (private) | GitHub commits and permissions |
| Knowledge repository | `dbontr/numerical-research-knowledge` (private) | GitHub Git history, independent of source repos |
| Libraries | `benchmark-corpus`, `research-papers` | Workspace shared-library membership and immutable sources |
| Planning | A selected GitHub Projects V2 board | GitHub Project ID, items and fields |
| Packages | Source-linked OCI images or other GitHub Packages | GitHub package registry and package permissions |
| Artifacts | Results, datasets and binary checkpoints | Workspace content-addressed storage and verified digests |

Links are relationships, not ownership transfers. A repository may appear in several projects. One library may serve several projects. A GitHub Project can track Issues and PRs from many repositories. Do not manufacture a one-to-one project/repository mapping or clone an independent repository merely to populate a project.

## Library-focused projects and reusable publications

A **library project** is a complete Workspace project that publishes a reusable library. It can own source repositories, optional private knowledge, issues, a GitHub Projects V2 board, experiments, tests and builds, just like any other project. Its published library is a separate **consumer-facing collection** of pinned Git sources and digest-verified objects. A different project may link this library without becoming a collaborator in the publisher project.

Example: create project `lean-proofs` with `kind=library` and `library_kind=lean`. It publishes library `lean-proofs`. Link source repository `owner/lean-proofs` to the **publisher** and register an exact Git commit from its Lean files through `library_add_source`. Projects `formal-verification` and `quantum-algorithms` independently call `library_link` for `lean-proofs`, resolve its pinned sources through `library_resolve`, and import only that authorized material. The publisher's private knowledge and GitHub repo write access are not granted to consumers.

The controller offers `create_library_project` (MCP) or `POST /api/projects` with `kind=library` and `library_kind=lean|research|code|mixed`. It atomically creates the Workspace project, a same-ID library publication and the publisher's own library link. `project_published_library` (or `GET /api/projects/{id}/published-library`) resolves the publication. An ordinary project is `kind=standard` even if it consumes multiple libraries.

Library-only projects do not receive automatic K3s workers unless the operator explicitly includes their project ID in the provisioner's `computeLibraryProjects` allowlist. This avoids spending four compute-worker deployments on a passive proof catalog, while allowing a proof-library project to request its own workers when compilation or formal verification warrants it. Standalone libraries without a publisher project remain valid.

A linked library currently exposes its **shared published source revision**; updating a library's source reference changes the revision seen by linked consumers. For reproducibility, resolve and save the exact Git commit in each consuming project's build, experiment or dependency manifest. Treat independent consumer-specific release pinning and publication promotions as additional features unless the live controller explicitly implements them. A GitHub release/package may convey the public library interface, but does not by itself grant access to the publisher's private knowledge.

## Project creation and linking

1. Use `list_projects`; select an existing stable project ID when it fits. Otherwise create a `standard` project or an explicitly selected `library` project only when authorized. A library project atomically publishes a same-ID reusable library. Choose an ID from the **research or work objective**, not a source repository name or the first task.
2. Use `repository_memberships` and `git_provider_capabilities` before linking. Verify each intended GitHub repository through the provider and record its numeric `id`, `full_name`, default branch, permission and role. Use `git_link_repository` or `link_github_repository` as appropriate. A loose link is valid; it need not copy source or create a checkout.
3. Assign each repo its actual role: e.g. `source`, `research`, `documentation`, `hardware`, `lean` or `planning`, according to supported schemas. Never infer access to another repository from one successful link.
4. Use `library_list` to discover reusable libraries. Create one with `library_create` when warranted and authorized. Add immutable Git source commits or digest-verified objects; use `library_link` to authorize a project's access. Use `library_resolve` for source provenance rather than copying all library records into a project's knowledge repository.
5. If planning is wanted, attach an existing, explicitly selected GitHub Projects V2 board with `github_project_attach`, then inspect `github_project_board`, `github_project_fields` and `github_project_items`. Do not guess a board ID or create a board as a proxy for Workspace project creation.
6. Select knowledge independently: `none` or `git`. For private knowledge, select a repository whose actual GitHub `private` field is `true`, with explicit branch and prefix. The default proposed name is `OWNER/PROJECT-ID-knowledge`. Do not derive it from any source repository; do not assume the destination exists before provider confirmation.

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

**Default design:** optional, independent, private GitHub repository per Workspace project. The source repositories may be public, private or mixed. A single private knowledge repository can also hold multiple approved project prefixes when expressly selected, but do not share its permissions unintentionally.

Visibility is enforced by GitHub, not by a directory, branch or skill. For confidential knowledge, verify the repository itself is private before both `knowledge_configure` and `knowledge_git_publish`. A `knowledge/` branch inside a public repository remains public. A public GitHub Project board, issue, PR, Action log or package metadata may also disclose content copied from private knowledge. Never publish private reasoning, context, findings, credentials or draft ideas there. Refer to private knowledge by a non-sensitive identifier only when that reference is itself safe to disclose.

Use `Project.md` to hold the project charter, authoritative links and stable identities. Use `Context.md` for current verified state, not a transcript. Keep evidentiary records in the knowledge standard's `decisions/`, `findings/`, `experiments/`, `sources/` and `handoffs/`. Do not automatically generate these files when a task completes; an explicit knowledge decision is required.

When migrating knowledge out of a public repository, establish and verify the private destination **before** changing the knowledge policy. Read back every migrated file at the private commit. Then retire the public branch or content through an authorized cleanup. Deleting a Git branch cannot retract data already fetched or copied; historical exposure must be treated as such, not called fixed by deletion alone.

## Cross-repository GitHub workflow

A Workspace project can have one GitHub Projects V2 board across multiple source repositories. Plan high-level dependencies on the board; use the relevant repository's Issues for implementation, discussions for open-ended debate when appropriate, and PRs for review of a particular repository's source. Only use matching package, release, CI, security and dependency tools when the linked repository and credential actually authorize them.

GitHub Packages (including GHCR) is a publication/distribution authority, not the project knowledge store. Treat package visibility and permissions separately; an image linked to a public repository is not necessarily public, and private repository membership does not automatically grant every runtime package access. Resolve package access explicitly before claiming a deployable environment.

## Scope and cleanup

The owner controls project names, access boundaries and knowledge settings. The controller owns operational state and leases; GitHub owns repositories and planning; libraries own reusable references; artifacts own binary bytes. Do not mirror an authoritative issue backlog as a Markdown task list or duplicate package binaries in knowledge Git. Do not create a project merely to retain conversation context. A valid output may be a code change, artifact, issue/PR, library object, experiment record or no durable record at all.
