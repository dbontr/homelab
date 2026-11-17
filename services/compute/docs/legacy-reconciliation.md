# Historical project reconciliation and recovery

## Scope

Project catalog visibility is a presentation policy, not data deletion or worker retirement. The owner-only revision-fenced catalog operations from PR #197 are the authoritative interface. The project identities `acceptance`, `workspace-mcp`, and `health-stack` may be hidden only after the approved consolidation gates; they must retain direct historical access, provenance, and independent recovery archives. `homelab-platform` must remain visible. An existing Git repository is not, by itself, evidence of a distinct project identity.

The `legacy-reconcile` command performs a **read-only** comparison between explicitly inventoried legacy files and an independently restored copy. It neither imports records nor changes project visibility. The operator must supply two independently generated manifests with the same immutable record identifiers, revisions, timestamps, project identities, relationships, byte sizes, and SHA-256 digests. Each file must be classified; any unclassified source file prevents a successful audit.

## Manifest contract

Each manifest is JSON with `version: 1` and `records`. A record contains:

| Field | Requirement |
| --- | --- |
| `legacy_id` | Original source-owned record identifier, not a repository name |
| `revision` | Original revision or immutable source version |
| `project_id` | Explicit project association from the authoritative source |
| `kind` | Source record kind, such as `note`, `artifact`, or `job-result` |
| `created_at` | Source timestamp in RFC 3339 format |
| `source_path` | Canonical relative file path inside the copied source tree |
| `restore_path` | Canonical relative path inside the independently restored tree |
| `sha256` | Lowercase hexadecimal SHA-256 of both files |
| `size` | File size in bytes |
| `links` | Optional `legacy_id@revision` relationship targets |

The manifests must themselves be retained with source inventory and restore evidence. The report includes the original manifest SHA-256. Records cannot use absolute paths, traversal, or symlinked file components. Source and restore must be separate trees; hard-linked copies do not count as independent restoration.

## Isolated verification

Use the checked-out source with the pinned Go toolchain:

```sh
cd services/compute
go test -count=1 ./cmd/legacy-reconcile
go run ./cmd/legacy-reconcile \
  --source-root /path/to/copied-legacy-source \
  --manifest /path/to/legacy-manifest.json \
  --restore-root /path/to/independent-restore \
  --restored-manifest /path/to/restore-manifest.json \
  > /path/to/reconciliation-report.json
```

The command returns nonzero when any record, relationship, pathname, digest, size, independent restore, or completeness check fails. Its JSON report separates verified records from exact finding codes. The program never creates, updates, or deletes input files. The input directories must be copies or verified read-only mounts, **never** the production `/srv/project-source` tree.

Four standalone Go tests pass in a disposable local runner using `GO111MODULE=off go test -count=1 -v`: independent restored bytes and provenance; duplicate identities and metadata conflicts; unmapped/missing/invalid paths; machine-readable read-only CLI output. A complete pinned module test pass, real production-source manifest, and actual off-host restore audit remain pending.

## Migration inventory and acceptance status

| Subject | Evidence | Status |
| --- | --- | --- |
| Catalog visibility and rollback | PR #197 contains owner-only revision-fenced hide/unhide, listing filters, and temporary-database tests | Implemented in source; full module suite and protected live authorization not certified here |
| `acceptance` and `workspace-mcp` artifacts | Read-only controller inventory: all 10 and 16 original artifact metadata records have exactly one target in `homelab-platform`, with matching source-ID-prefixed filename, SHA-256 and size; two sampled pairs also passed byte-for-byte readback | Exact catalog metadata match 26/26; all-byte independent off-host restoration remains pending |
| Historical Jobs and knowledge | Issue #196 inventories 11 acceptance Jobs, 4 workspace Jobs, preserved Git knowledge and immutable reference records | Source IDs, job receipts, revisions, relationships and cross-chat retrieval require independent comparison |
| `health-stack` historical records | Still has associated worker registrations and possible historical dependencies | Not reconciled; do not retire |
| `/srv/project-source` | Read-only issue #196 inventory reports about 147 MB, three historical repository groups and content-addressed artifact groups | Full manifest and bytes not provided to this isolated workstream; do not claim imported |
| `/srv/project-knowledge` | Issue #196 reports the former path absent on November 17; older 5.5 GiB estimate is historical | Provenance and any off-host copy not yet resolved; no deletion permitted |
| Independent off-host recovery | Issue #196 points to Janus age archives and issue #192 | Encrypted scheduled backup and post-consolidation restore are outstanding gates |
| Record search, file revisions, task/dataset links, compute results | Existing controller offers project knowledge, artifacts, libraries, research index and durable Job lookup | No complete legacy-to-current relationship audit or fresh-chat restore certificate |

This inventory distinguishes historical assertions from records independently verified in this development branch. A separate read-only live controller inventory confirmed seven visible IDs (`acceptance`, `aup-zu3-pynq`, `health-stack`, `homelab-platform`, `minecraft-rust`, `web-game-console-emulator`, `workspace-mcp`), 10 acceptance artifacts, 16 workspace-mcp artifacts, 0 health-stack artifacts, 68 homelab-platform artifacts, 11 historical acceptance Jobs and four historical workspace-mcp Jobs. Every one of the 26 source artifacts had exactly one source-ID-prefixed target with matching displayed SHA-256, filename and size. Two small independent source/target artifact reads yielded identical base64 payloads and metadata. These findings are not proof of a complete off-host restore or reconciliation of unindexed filesystem contents; the local test suite did not access production or mutate any records. A separate fresh-chat read-only lookup also recovered immutable historical knowledge contribution `2243d4fbc68836e8d60550e09bdc73df` with its preserved source Git locator, and original Kueue Job `40ed5c6627f5446a3fe05d7f0a6583c9` as `succeeded` with its durable controller-restart result. This verifies individual cross-conversation reads, not complete recovery of the entire historical catalog.

## Owner-approved migration sequence

1. Inventory the exact legacy source and immutable archives in read-only mode. Classify every record and preserve original timestamps, identifiers, digests, ownership, source paths, Git commit IDs, and relations; do not infer project IDs from directory or repository names.
2. Create the independent encrypted off-host snapshot required by #192. Restore to disposable storage and perform SQLite integrity checks, original receipt lookup, file and artifact checksums, project relationships and worker-resource inventory.
3. Export separate manifests from the original source and restored copy, then run `legacy-reconcile`. Investigate every `finding`, including unmapped source records, before approval. Keep both manifests, the command output, and exact repository commit as evidence.
4. Verify no live workspaces, Jobs, hardware leases or active project-specific workers depend on any project proposed for retirement. Preserve Jupiter and Janus EDA reservations. Resolve worker registrations through a separately authorized and reversible operation.
5. Using the independent owner security authority, hide **only** `acceptance`, `workspace-mcp`, and `health-stack`, using the current revision for each. Confirm ordinary lists omit them, direct historic reads still work, fresh-chat queries resolve original records, and unhide/rollback restores listings.
6. Keep historical originals and off-host archives. Any permanent deletion or source repointing requires a distinct owner-approved change; catalog visibility never authorizes deletion.

Do not merge, deploy or run production catalog mutations as part of this branch. See issues [#196](https://github.com/dbontr/homelab/issues/196) and [#192](https://github.com/dbontr/homelab/issues/192), and merged [PR #197](https://github.com/dbontr/homelab/pull/197).
