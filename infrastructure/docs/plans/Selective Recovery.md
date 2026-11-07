# Selected-state recovery

## Recovery set

| Resource | Authority | Selected encrypted archive |
| --- | --- | --- |
| NixOS configuration and flake lock | Pinned GitHub history | Git bundle |
| RecipeSage | PostgreSQL | Consistent pg_dumpall export |
| Adaptive Teaching knowledge, evidence and attachments | SQLite | SQLite online backup when its deployed path is set |
| Legacy Project Knowledge | Saturn project directory | Directory archive until GitHub migration is verified |
| Opt-in Project Knowledge | Configured Git remote, GitHub on this installation | No local backup required only after the intended Git commits and readback are verified |
| Media, downloads and disposable worker output | Application-specific | Excluded |
| Bootstrap identities and recovery secrets | Protected secret escrow | Not included in the database archive |

A flake does not restore Cloudflare tunnel identity, cluster trust, VPN keys, external DNS configuration or physical storage layouts. Do not stop the existing broad backup timers until the selected archive has passed a real restore rehearsal and separate bootstrap identities are recoverable.

Project Workspace's SQLite database records projects, tasks, leases and artifact references, while artifact bytes live outside that database. Its state is **not included** in the selected-state archive. The controller supports independent encrypted Restic snapshots of its database and referenced artifacts. The production NixOS module requires `homelab.projectWorkspace.backupInterval`, `homelab.projectWorkspace.offsite.repository` and a preprovisioned password Secret. A nonempty interval does not prove the destination is reachable: verify an encrypted snapshot on the independent host and a full isolated restoration before removing the former compute broker. GitHub source history cannot reconstruct these controller records or artifact bytes.

## Service configuration

The NixOS option homelab.recovery.selected.enable is opt-in and remains disabled during migration. Configure homelab.recovery.selected.learningDatabase to the deployed Adaptive Teaching SQLite path after deployment. An unset path is reported as learning=not_configured and does not contain learning state. For a separately staged learning service, pass its verified database path explicitly when running a manual backup; original image and document attachments are SQLite blobs in Adaptive Teaching and are preserved by the consistent SQLite snapshot.

The selected archive defaults to excluding local project knowledge, because Git is the authority for new projects. On a legacy installation where local knowledge remains authoritative, set `homelab.recovery.selected.legacyProjectKnowledge = true` explicitly until every intended Git commit and readback has been verified. If knowledge persistence is disabled, do not create new knowledge records. If it is enabled, retain old local notes until their intended Git commits have been verified and read from a fresh checkout. The script requires a non-shallow Git checkout, an age recipient, and the existing authenticated Janus SSH transport. It captures tracked modifications and nonignored untracked configuration separately from the Git bundle. Required backup tools are checked before staging data. It currently exports RecipeSage from the established PostgreSQL container. If that deployment changes, update the integration before activation.

The backup stages the Git bundle, PostgreSQL export, an optional consistent SQLite snapshot and legacy knowledge, verifies contents and SHA-256 entries, encrypts the archive with age, copies it to Janus, verifies its ciphertext hash and confirms remote decryption. Plaintext staging is deleted. Archive pruning requires a separately reviewed retention policy.

## Restore rehearsal

1. Decrypt one archive on the independent recovery host and verify SHA256SUMS.
2. Clone the Git bundle into an isolated directory, inspect the commit and evaluate each NixOS target.
3. Restore the RecipeSage database into a separate PostgreSQL instance and verify normal recipe reads and edits.
4. Restore the learning database into an isolated application and verify graph, evidence and attachment integrity.
5. Restore the legacy project directory, or compare a fresh GitHub checkout with the source inventory.
6. Restore a Project Workspace Restic snapshot to a fresh controller data directory, verify referenced artifact checksums, and demonstrate a task read through the restored service.
7. Restore bootstrap identities from protected escrow, verify K3s and networking, and only then switch production traffic.

The independent recovery host can validate an encrypted archive without replacing any live services. After installing `age`, Git, GNU tar, SQLite 3, xxd, gzip, coreutils and awk and obtaining the reviewed homelab source, run:

```sh
RECOVERY_RESTORE_SCRATCH=/srv/saturn-encrypted-backups/selected \
  bash homelab/verify-selected-restore.sh --require-learning --require-legacy \
  /srv/saturn-encrypted-backups/selected/ARCHIVE.tar.age
```

The verifier checks the archive against its complete SHA-256 manifest, restores the Git bundle to an isolated repository for bundle verification, checks PostgreSQL gzip integrity, performs SQLite integrity checks, and verifies the original attachment digests when a learning database is present. A free-space preflight and private temporary directory protect the recovery host. This is a *content integrity rehearsal*, not a restoration of RecipeSage or the learning server as a live application; those remain separate acceptance tests.

A successful archive transfer is not yet a complete disaster-recovery test. The Project Workspace acceptance controller has passed one encrypted Janus restore rehearsal (manifest, SQLite integrity, and two recorded jobs); it did not contain retained artifact bytes, and recurring backups are not yet enabled. Retain the existing backup system until isolated restore proves the selected recovery contract.
