# Project execution and knowledge contract

Workspace projects are stable coordination scopes, independent of source repositories. A project may have no repository, several repositories, or publish a library for other projects. One-off tasks do not require projects, and no task automatically becomes a knowledge record.

## Execution

A standalone task may request CPU, GPU, NPU, FPGA or USB resources under explicit lease and admission rules. Its files are returned or deliberately exported; a temporary workspace does not become the durable project authority.

External equipment, EDA environments and remote clusters remain outside the free compute pool unless explicitly registered and approved for a specific operation. Registration alone never authorizes execution.

## Storage and authority

The controller maintains project metadata, notes, immutable contributions, library links and research indexes in SQLite. Library documents, datasets and other immutable objects use content-addressed storage with SHA-256 verification. Neither requires a GitHub repository.

A project's knowledge policy is independent of its code links:

| Mode | Canonical knowledge | Permitted operations |
| --- | --- | --- |
| `local` (default) | Controller SQLite notes and immutable contributions | Versioned note reads/writes; sourced contributions; search and context |
| `none` | No canonical project knowledge | Metadata and ordinary project operations only |
| `git` (explicit) | Approved GitHub or local Git repository | Pinned document reads and expected-head publication |

For `local`, the conventional note names are `project` (charter) and `context` (verified state and next action). Notes use a required `expected_version`; a stale write must fail. Contributions use `handoff`, `decision`, `finding`, or `note` and retain author principal, immutable source identity where supplied, and a SHA-256 body digest. Missing source evidence is never invented.

For `none`, the canonical context contains no local document or contribution. Existing records may remain retained for an authorized restoration but cannot be read or changed through the project knowledge API while the policy is disabled. Existing explicitly configured `git` projects retain their selected repository authority, and previously local content is not presented as Git-authoritative knowledge.

## Shared libraries

A library project can publish a reusable library using the same stable ID. Linked consumer projects receive read access to its published sources and immutable content objects, not permission to edit the publishing project or its linked code repositories.

A library can contain any combination of:
- Local immutable `.md`, `.txt`, and `.json` documents, identified by names and SHA-256 digests.
- Local datasets, papers, study packs and result objects uploaded through verified file-transfer operations.
- Explicitly pinned Git sources for libraries whose reusable product is source code.

Use `library_document_publish` as an editor of the **publishing project**; use `library_documents` and `library_document_read` as a member of any linked project. Document names are immutable. A revised publication must have its own name and digest. Large binary artifacts remain in the existing object store rather than SQLite note fields. Use `library_link` and `library_resolve` to share references, not copies.

## Source control and provenance

GitHub remains an optional source-code and collaboration provider: issues, PRs, branches, releases, packages, and planning boards belong to their appropriate repositories. A linked code repository does not control project knowledge. Verified Git commits, imported data checksums, report methods and immutable artifact references can appear **as provenance** within controller-held notes and documents.

A file-capable MCP client must send actual bytes before an attachment can be stored. Research indexes track original source identity, passage hashes and revisions; do not invent source locations or treat an index as the authoritative original.

## Migration and recovery

Do not switch the knowledge policy for an existing Git-backed project merely because local storage is available. Enumerate existing knowledge documents at a pinned commit, verify contents and permissions, import them with their real provenance, check readback, and only then change that project's policy with its expected version. Retain original repositories and objects until an authorized independent restore test succeeds. A local database and its objects need a tested off-host recovery plan; hosting on the controller does not constitute a backup.

Acceptance requires real authorization-negative tests, integrity and concurrency checks, and live readback after a development deployment. The source code alone does not establish that the migration or deployed controller is operational.
