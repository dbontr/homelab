# Project Knowledge Standard

## Authority

Project knowledge is private by default to the controller's authenticated project grants. A Workspace project is a durable identity separate from its source repositories, planned compute work, linked libraries and GitHub boards.

The controller's `knowledge_policy` has three modes:

- **`local` (default):** canonical notes and immutable contributions are held in the Workspace controller's private SQLite database.
- **`none`:** no canonical knowledge is exposed or accepted; existing data is retained unless independently removed through an authorized procedure.
- **`git` (explicit):** a verified repository, branch and prefix own knowledge for that project. It may be GitHub or an approved local bare Git repository. A previously configured Git destination remains authoritative until a verified migration is completed.

Never substitute a public code repository for a private knowledge store. A GitHub branch is no more private than its parent repository. Do not place private reasoning, unsubmitted findings or private paths in issues, PR comments, GitHub Projects fields or workflow logs.

## Local project records

The controller stores editable notes by stable `project_id` and `name`, with a required expected version. Two conventional names identify the main documents:

- **`project`:** charter, scope, acceptance criteria, durable external authorities and linked library purposes.
- **`context`:** verified state, work in progress, blockers, next action and exact source references.

Use `read_project_note` before `write_project_note`, and supply the returned `version` as `expected_version`. Never overwrite a concurrent edit by guessing a revision. A missing note begins at revision 0. A successful write increments the revision; it is not a Git commit.

Immutable contributions preserve findings and handoffs separately from the current context. Use `knowledge_contribute` with `kind=handoff|decision|finding|note`, a meaningful title, and a body containing the relevant evidence. The controller binds the authenticated principal and body SHA-256; do not supply a claimed author or alter provenance after publication. Optional `repository` and `revision` must identify a linked source repository at an exact commit; an optional artifact ID must belong to the project.

A complete contribution gives a reader enough information to distinguish **observed evidence**, **derived results**, **hypotheses**, and **accepted decisions**. Use specific numerical units, limitations, uncertainty and reproducible procedures where applicable. A completed experiment is not automatically a verified scientific conclusion. Record corrections and conflicting evidence in later contributions rather than altering immutable records.

### Project note example

```markdown
# Project

## Purpose
Develop a verified numerical library for bounded operators.

## Scope
Implementation, proofs and numerical acceptance tests.

## Acceptance criteria
State the measurable conditions and the evidence that demonstrates them.

## Authorities
Workspace project ID, linked code repositories, pinned library sources and relevant artifact digests.
```

### Context note example

```markdown
# Context

## Verified state
Link results to exact source commits, reports and immutable object hashes.

## In progress
Describe the active task without guessing its outcome.

## Blockers
Identify unverified dependencies and missing resources.

## Next action
Specify one concrete operation and its acceptance criterion.

## References
Use exact source identities or citation records.
```

The examples establish structure; they do not imply that a project has those results. Keep notes concise and factual. Do not paste chat transcripts, fabricated test data, whole logs or temporary scratchpads.

## Shared libraries

A library is a separately published, reusable collection, not a directory in a project's code repository. A `library` project publishes its own named library; other projects can link it through `library_link` without gaining permission to edit the publisher.

Shared library content uses the Workspace content-addressed object store and exact SHA-256 digests. Publish documents through `library_document_publish` (UTF-8 Markdown, JSON or plain text up to 128 KiB). Readers use `library_documents` and `library_document_read` only for libraries linked to their project. Names are immutable: a revised theorem, proof or dataset description receives its own versioned name or edition identifier, and consumers can cite the exact digest. The system does not silently rewrite published objects.

Large papers, datasets, model weights, bitstreams and saved experiment outputs belong in the verified object/artifact store. The companion document can contain their immutable object identifier, digest, size, original locator, acquisition date and permitted use. Research passages are secondary indexes with explicit original-source provenance, not replacements for the original source.

Pinned Git repositories remain appropriate for **source-code libraries**. `library_add_source` may register an exact code commit without moving the project's private knowledge to GitHub. A consumer sees the published source revision and the locally stored objects through `library_resolve`. Do not infer that linking a library grants permissions in the original code repository.

## Reproducibility and scientific provenance

For technical findings, preserve the exact relevant facts:
- Source commit, versioned OCI image or environment specification.
- Input source, immutable SHA-256 identifier, location and permitted scope.
- Hardware and device allocation actually used, not merely declared inventory.
- Driver/toolchain versions, relevant environment variables without secret values, and the command or method.
- Measured outputs, units, sample size and uncertainty.
- Reproduction instructions, failure modes and acceptance criteria.

Use verified citations and links to original material. Never invent pages, DOI values, terminal outputs, commits, results or timestamps. If the evidence is unavailable, state that the finding remains provisional.

## Git-backed knowledge where explicitly configured

Projects in `git` mode use the approved repository, branch and knowledge prefix. Prefer a private destination for confidential material. Common portable record filenames include `Project.md`, `Context.md`, `decisions/`, `findings/`, `experiments/`, `handoffs/` and `sources/`. A record may use frontmatter such as:

```yaml
---
id: transfer-study-20261105
kind: experiment
status: provisional
date: 2026-11-05
---
```

The record's immutable ID, date and evidence must remain consistent. Valid scientific record states include `proposed`, `provisional`, `verified`, `superseded` and `archived`. A `verified` finding requires reproducible evidence, not merely a successful computation.

Git publication requires the expected branch head, a non-forced update and verified readback. Local controller notes are not authoritative while a project's mode is `git`; do not mix both modes in one claimed history. Git-backed records remain optional and do not replace the Workspace shared library object store.

## Migration and independent recovery

Before changing a Git-backed project to `local`, enumerate the approved knowledge tree at a pinned commit and preserve its original file identities, hashes and source histories. Import intended project and context notes with revision guards; import findings or handoffs as immutable local contributions with explicit provenance. Read back and compare the new records, verify that linked artifacts remain accessible, and test restoration before switching the authoritative policy. Do not silently convert historical documents into accepted facts.

Reconfiguring a policy never copies or deletes content automatically. Keep the original repository and data until an owner-authorized removal procedure succeeds. Database and object-store recovery must be tested together; a local copy without off-host recoverability is not a backup.

## Professional record hygiene

Knowledge files and notes contain enduring project information, not evidence that an assistant was involved in writing them. Respect existing author attribution, required licenses and audit trails; do not fabricate or erase provenance. Store secrets only in the credential manager, not notes, documents, Git history, object manifests or logs. Keep decisions at their true scope and do not duplicate live GitHub issue state in local research notes.
