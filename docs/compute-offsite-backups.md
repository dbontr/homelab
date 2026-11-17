# Compute project recovery

Compute owns the authoritative project SQLite state and checksum-addressed
artifacts in the `pwm-controller-state` PVC. Its in-process Restic scheduler
creates a consistent snapshot while preserving the controller's running
Kubernetes Jobs. Neither the MCP gateway nor a worker is a backup authority.

## Independent storage

Janus runs a dedicated `compute-backup` SFTP identity confined to
`/srv/compute-restic/repository` by an OpenSSH chroot and
`internal-sftp`. The account cannot use interactive SSH, port forwarding or
a remote command. Its `authorized_keys` file is provisioned locally at
`/etc/ssh/authorized_keys.d/compute-backup` and must not be
committed to Git. Protect this file as root-owned, mode 0600. The server's
host-key fingerprint must be verified independently before writing a pinned
`known_hosts` entry.

Use a unique Ed25519 client identity solely for the backup controller. Do not
reuse any root or operator key. Keep the SFTP private key, pinned `known_hosts`
and random Restic repository password in separately provisioned Kubernetes
Secrets in `pwm-system`. The chart expects:

- `controller.offsite.passwordSecretName`: a `password` data key;
- `controller.offsite.sshSecretName`: `id_ed25519` and `known_hosts` keys;
- `controller.offsite.alertSecretName`: a separate `token` data key;
- `controller.offsite.alertURL`: the approved HTTPS notification receiver.
- `controller.offsite.repository`: `sftp:compute-backup@10.77.30.232:/repository`.

The chroot path is interpreted inside the independent Janus SFTP service.
Before enabling the scheduler, confirm that the selected Janus filesystem
has enough space for multiple generations of project data and restore
scratch. Create and initialize the Restic repository with the dedicated
identity and protected password, then verify its exact URL from within the
same controller container environment. Use strict SSH host-key checking.
Do not put password, SSH key or token bytes in Helm values, Nix source,
logs, GitHub issues or MCP tool arguments. The alert receiver URL above is an
example only, not a configured or authorized endpoint. The source overlay
leaves the alert URL and Secret name empty, and Helm must refuse activation
until these values are supplied through the protected release process.

## Declarative configuration

The source-owned Compute Helm chart supports the following values:

```yaml
controller:
  backupInterval: "24h"
  backupRetention: true
  offsite:
    repository: "sftp:compute-backup@10.77.30.232:/repository"
    passwordSecretName: "pwm-compute-restic-password"
    sshSecretName: "pwm-compute-restic-ssh"
    alertURL: "https://backup-alerts.example.invalid/hooks/compute"
    alertSecretName: "pwm-compute-backup-alert"
```

The running production controller is managed by the existing `pwm-controller`
Helm release in `pwm-system`. Its NixOS `services.workspaceMcp.enable` is
disabled; **enabling the NixOS module is not part of this rollout**. The
source-owned overlay is
`infrastructure/homelab/workspace/controller-backup-values.yaml`.
Apply it only as a values overlay to the *verified existing Helm release*,
preserving the installed controller OCI image digest, Kueue/RBAC, Access,
persistent-volume claim and other credentials. Review the exact diff between
`helm get manifest` and the rendered chart before any approved upgrade. The
source-built controller image must include the intended scheduler and retention
implementation; a Helm chart change alone does not update its executable.

For alternative installations that deliberately enable the NixOS module,
the matching options are `services.workspaceMcp.backupInterval`,
`backupRetention`, and `offsite.*`. These are configuration examples,
not proof that the live release has these Secrets or settings. Do not activate
until the existing Helm values, controller image digest, PVC, credentials
and controller execution receipts are verified. Render the proposed template
and compare the complete Deployment to its existing Helm release before
`helm upgrade --atomic --wait`. The NixOS deploy service fails before Helm
if an enabled backup Secret is missing.

The scheduler snapshots immediately on controller start and on each interval.
It attempts up to two five-minute retries for a failed snapshot and then
returns to its normal interval. Each attempt has a 12-hour maximum duration.
On success it verifies the encrypted repository, and it performs a full
`restic check --read-data` after controller startup and at least every seven
days. A full-data check failure is recorded as a failed backup cycle.
Successful verified snapshots can trigger scoped retention of the tagged
Compute backup set. Retention keeps
at least two latest, seven daily, five weekly and twelve monthly snapshots;
the policy groups by tag rather than the ephemeral Kubernetes Pod hostname.
The `forget` operation never targets untagged or unrelated snapshots. It
runs only after a successful encrypted backup and repository integrity check.
An isolated operator can also invoke `project-workspace-controller prune-offsite`
with the same protected repository environment.

## Acceptance and recovery

1. Before changing production, inspect `pwm-controller-state`, its existing
   Helm release, readiness, owner approval and the Janus recovery filesystem.
   Preserve the previously verified age-encrypted recovery archive.
2. Render the desired Deployment and confirm all secrets are present by name,
   without printing Secret contents. Validate the restricted account cannot
   execute shell commands, forward ports or write outside its repository.
3. Stage and activate only through the independent owner-authorized release
   path. Observe an actual scheduled Restic snapshot ID in the controller log,
   including recovery after a deliberately unavailable repository.
4. Run `restic check --read-data` with the protected client identity. Verify
   the exact saved snapshot ID using `project-workspace-controller verify-offsite
   SNAPSHOT_ID`. This restores into disposable storage and checks both the
   manifest hashes and SQLite integrity.
5. Independently restore that exact snapshot on a separate empty recovery
   directory, compare project/knowledge/artifact counts, verify a known
   SHA-256 digest and recover a durable compute-job result. Never restore
   into the active controller PVC during qualification.
6. Before enforcing retention, inspect `restic forget --tag project-workspace
   --group-by tags --keep-last 2 --keep-daily 7 --keep-weekly 5
   --keep-monthly 12 --dry-run`, then verify older and unrelated
   snapshots remain according to policy. Record disk capacity and
   alert on missing new snapshot receipts or repeated backup failures.
7. Confirm five Kubernetes nodes Ready and all 24 registered Compute workers
   healthy; preserve the previous Helm and NixOS release for rollback.

**Production acceptance requires an observed scheduled snapshot and a
separate verified restoration.** A source build, a successful manual archive
or a Helm rollout alone does not establish unattended recovery.

## Unattended health, alerts and recovery records

When enabled, the controller writes a private, atomic
`.compute-backup-health.json` receipt beside its SQLite database. This
persisted state tracks the last attempted and successful snapshot, full-data
verification time, consecutive failures and outstanding alert. The receipt
survives controller replacement and permits detection of an interrupted or
missed daily schedule. It contains no credential bytes or database contents.

The backup receiver must support authenticated HTTPS POST requests carrying
`Authorization: Bearer <dedicated-token>` and JSON with these string fields:

```json
{"source":"compute-backup","condition":"repeated_failures","status":"firing","observed_at":"2026-01-01T00:00:00Z"}
```

Supported conditions include `missed_backup` (no successful receipt within
twice the interval), `repeated_failures` (three consecutive failed attempts),
`insufficient_capacity`, and `corrupt_snapshot`. On recovery, the controller
posts the matching `resolved` event. A failed notification is logged and can
be retried. The receiver must authenticate the token, deduplicate transitions,
notify the operator, and preserve its own independent alert history. Only a
2xx response acknowledges delivery. The receiver itself must be provisioned
and independently accepted before enabling unattended backups; a syntactically
valid HTTPS URL is not evidence of notification delivery.

Janus also exposes a source-owned Prometheus alert when its backup-bearing
root filesystem has less than 40 GiB available. The coordinator must validate
that `/srv/compute-restic` remains on that filesystem, check Alertmanager or
Grafana contact delivery, and adjust the threshold if its mount changes. A
backup failure or low capacity does not authorize deleting the protected
age-encrypted recovery archive.

## Protected activation sequence (coordinator only)

This procedure describes a future independently authorized cutover. Source
publication and test results never grant permission to activate services.

1. **Freeze and record the baseline.** Resolve exact main and candidate
   commit IDs; save the complete existing Helm values, Deployment and release
   revisions, controller OCI digest, source and bound PVC identity,
   `pwm-controller-state` capacity, Janus running and boot NixOS generation,
   active Vivado hardware services, Kubernetes and worker readiness. Verify
   that the preserved age-encrypted archive is still intact. Do not access
   the archive contents.
2. **Verify identity and capacity.** With an independent administrator
   approval, reconcile the Janus boot-generation drift and evaluate the
   restricted `compute-backup` Nix module. Before any activation, verify
   chroot parents are root owned, repository directory is writable only by
   the dedicated account, port forwarding and shell commands are disabled,
   and the source-pinned SSH host fingerprint is confirmed out of band.
   Check actual Janus free space and the 40 GiB warning floor.
3. **Provision protected credentials.** Use the security-owned credential
   path to create an independent repository password, dedicated Ed25519
   client key, pinned host-key file and dedicated alert token. Store secrets
   separately in `pwm-system` Kubernetes Secrets. Create the root-owned
   Janus authorized-key entry through an approved host operation. Never use
   the host's root SSH key or expose secrets in command arguments, logs,
   source control or this document.
4. **Prepare off-host storage.** After the restricted principal is active,
   initialize a *new* Restic SFTP repository at the verified Janus chroot
   path. Never reuse the manually encrypted recovery archive or overwrite
   another Restic repository. Establish the approved dedicated HTTPS alert
   receiver, test delivery and receipt of `firing` and `resolved` messages.
5. **Stage the exact release.** Build and verify a digest-pinned Compute
   controller image containing this source. Keep the existing Helm release
   name, its PVC, all unrelated values, owner authorization, Kueue settings,
   worker configurations, and workload identity unchanged. Populate the
   otherwise intentionally incomplete source Helm overlay with the approved
   alert URL and Secret name. Render with `helm template`, inspect a complete
   base-versus-candidate manifest diff, and reject any unrelated change.
   Do not invoke a direct `helm upgrade` or NixOS switch outside the
   independent exact-commit release authority.
6. **Authorize and activate.** Ask the independent Rust release authority
   for an operation-scoped owner approval. Have the privileged executor apply
   only the inspected release, with bounded health checks and an exact
   rollback target. Do not use NanoKVM to bypass this boundary.
7. **Observe a real scheduled backup.** Capture the controller's completed
   full 64-character Restic snapshot ID and persisted health receipt; confirm
   the off-host repository records it. Exercise outage/retry acceptance using
   an explicitly approved non-destructive fault-injection technique.
   Verify the alert receiver acknowledges both failure and recovery.
8. **Verify independent recovery.** From an independently isolated host or
   disposable container with dedicated read-only verification access, run
   `restic check --read-data`; restore the exact snapshot to empty storage,
   never the controller's live PVC. Confirm `PRAGMA integrity_check = ok`,
   `PRAGMA foreign_key_check` has zero rows, exact artifact and library
   object SHA-256 hashes, project note and contribution identities, and
   durable Kueue Job result and request receipts. Preserve the independent
   restore evidence and immutable snapshot identity.
9. **Inspect retention and capacity.** Dry-run the Compute-tagged forget
   policy, show multiple historical recovery points and confirm unrelated
   tagged snapshots survive. Run pruning only after source identity,
   capacity and dry-run inspection. Confirm fresh capacity metrics,
   notification contact delivery, five Kubernetes nodes Ready and all
   expected Compute worker instances healthy.
10. **Close the production gate only on evidence.** Record protected release,
    snapshot and exact restore audit IDs; preserve rollback options and the
    independent age archive. A successful source test or manual snapshot
    alone does not justify marking issue #192 complete.

To verify the implementation without touching production, run the controller
unit tests against a disposable SQLite database and local encrypted Restic
repository, inspect the Helm render with synthetic values, and perform NixOS
evaluation without activation. These tests must include an interrupted backup,
unavailable repository, incomplete manifest, corrupted database relationship
and retained independent snapshots. Do not expose production credentials to
synthetic tests.

## Recovery when the original controller is unavailable

Use an independent recovery host or disposable isolated container. Install
the versioned `project-workspace-controller` binary corresponding to the
snapshot's schema, `restic`, `jq`, `sqlite3`, and a read-only verification
credential.
The active controller, its token, and its PVC are not prerequisites. Set
`RESTIC_REPOSITORY` and `RESTIC_PASSWORD_FILE` through protected credential
custody. For an SFTP repository, configure an independent verification SSH
identity and pinned host key with strict host-key checking. Do not reuse an
operator's administrative SSH key or publish any credential contents.

The following procedure is read-only against the remote repository. The
recovery destination **must not exist** and must be on independent disposable
storage. Supply the exact 64-character lowercase hexadecimal `SNAPSHOT_ID`
recorded in the scheduled backup receipt. Never use `latest` or a shortened
snapshot prefix.

```sh
set -eu
: "${SNAPSHOT_ID:?provide a 64-character snapshot receipt}"
: "${RECOVERY_ROOT:?provide an unused independent recovery directory}"
command -v jq >/dev/null
test "${#SNAPSHOT_ID}" -eq 64
case "$SNAPSHOT_ID" in *[!0123456789abcdef]*) exit 2;; esac
test ! -e "$RECOVERY_ROOT"
install -d -m 0700 "$RECOVERY_ROOT"
restic snapshots --json --tag project-workspace \
  | jq -e --arg snapshot "$SNAPSHOT_ID" 'any(.[]; .id == $snapshot)' >/dev/null
restic check --read-data
restic restore "$SNAPSHOT_ID" --target "$RECOVERY_ROOT/restic"
export PWM_DB="$RECOVERY_ROOT/recovered/state.db"
project-workspace-controller restore "$RECOVERY_ROOT/restic/snapshot"
sqlite3 "$PWM_DB" 'PRAGMA integrity_check; PRAGMA foreign_key_check;'
```

Require `integrity_check` to return exactly `ok` and
`foreign_key_check` to return no rows. The restore command validates every
manifest SHA-256 and file size, rejects an existing destination, checks
logical project/artifact/Job ownership, and verifies contribution digests.
Inspect the recovered database and artifact tree as evidence without starting
another controller on the active production identity. Compare independently
captured project identities, note versions, contribution provenance, file
revisions, task links and completed Kueue request/result receipts. Retain
the exact snapshot ID, versioned binary digest, manifest hash and inspection
receipt in a protected audit record.

### Historical record boundary

The controller snapshot covers the SQLite database and its referenced
immutable artifacts and library objects. It does **not** automatically cover
untracked historical host directories or external Git repositories. For
legacy filesystem sources, acquire a separately authorized independent archive
and independently generated source and restored manifests with original
project IDs, revisions, timestamps, names and relationships. Run:

```sh
cd services/compute
go run ./cmd/legacy-reconcile \
  --source-root "$COPIED_LEGACY_ROOT" \
  --manifest "$SOURCE_MANIFEST" \
  --restore-root "$RESTORED_LEGACY_ROOT" \
  --restored-manifest "$RESTORED_MANIFEST"
```

Both trees must be separate, disposable copies. An empty or incomplete
inventory, a missing or unexpected file, mismatched relationship, altered
provenance, invalid digest, or non-independent restored file fails the audit.
Do not mark historic data migrated, hide source projects or retire workers
merely because current project artifact metadata matches. The original age
archive and existing legacy sources remain protected until independently
approved recovery acceptance.

### Capacity and retention admission

Measure the actual filesystem holding `/srv/compute-restic/repository`,
not merely Janus's root-volume utilization percentage. Record available
bytes, filesystem size, existing protected archive consumption, the measured
first encrypted snapshot footprint, expected unique changed bytes over the
retention window, and growth margin. Admit scheduled backups only when the
measured **additional** requirement fits above the 40 GiB minimum free-space
alert floor. Compression and deduplication estimates are not a guarantee of
available space. Make another observation after the first real snapshot.

Run the Compute-tagged `restic forget` policy with `--dry-run` before
authorizing retention. Review snapshot IDs by tag and verify that snapshots
with unrelated or absent tags are unaffected. Never prune another backup
owner's history to make Compute fit, and never remove the independent
age-encrypted archive as part of Restic retention.

A scheduled off-host snapshot, an exact independent restore, actual alert
delivery, and independently measured destination capacity are required
before unattended production recovery is certified.

## Reproducible isolated source qualification

Use a disposable, unprivileged source-pinned runner, **not** any production
Compute or Projects workload. The test harness creates temporary SQLite,
artifact and encrypted Restic storage. It never connects to Janus.

Prerequisites: the repository's pinned Go 1.27.3 toolchain, `restic` in
`PATH`, adequate disposable scratch capacity and the exact Git commit.
The Restic-backed tests skip when `restic` is missing; the following
explicit prerequisite prevents a skipped test from being counted as a pass.

```sh
command -v restic >/dev/null || exit 1
cd services/compute
go test -count=1 -v ./cmd/legacy-reconcile
go test -count=1 -v ./internal/controller -run \
  'TestEncryptedIntegratedResearchRecovery|TestEncryptedOffsiteBackupRestoreAndVerification|TestOffsiteRetentionPreservesIndependentSnapshots|TestRepositoryOutageDoesNotDestroyExistingEncryptedRecovery|TestInterruptedOffsiteBackupPreservesPreviousSnapshot|TestEncryptedRepositoryFullCheckDetectsSimulatedPackCorruption|TestRestoreRejectsManifestMissingDatabaseReferencedArtifact|TestRestoreRejectsInvalidForeignKeyRelations|TestRestoreRejectsOrphanDurableJobProject'
go test -count=1 ./cmd/project-workspace-controller
go vet ./cmd/legacy-reconcile ./internal/controller ./cmd/project-workspace-controller
```

The combined test independently exports historical artifact inventories
before and after recovery and compares all record metadata and bytes. It
requires the project note revision, experimental notebook provenance,
historical original/copy association, immutable contribution, two task
file revisions and the completed Kueue receipt to survive one encrypted
snapshot. An extra restored file must fail reconciliation.

Record each command's actual exit status, Go and Restic versions, snapshot
ID, tested commit, and evidence for negative cases. The code and commands
are an acceptance procedure, **not** evidence that the combined tests
have run. Source qualification does not substitute for the separately
authorized scheduled Janus snapshot and independent production restore.
