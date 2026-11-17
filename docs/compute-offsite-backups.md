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
- `controller.offsite.repository`: `sftp:compute-backup@10.77.30.232:/repository`.

The chroot path is interpreted inside the independent Janus SFTP service.
Before enabling the scheduler, confirm that the selected Janus filesystem
has enough space for multiple generations of project data and restore
scratch. Create and initialize the Restic repository with the dedicated
identity and protected password, then verify its exact URL from within the
same controller container environment. Use strict SSH host-key checking.
Do not put password, SSH key or token bytes in Helm values, Nix source,
logs, GitHub issues or MCP tool arguments.

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

The scheduler snapshots immediately on controller start and on each interval,
with five-minute bounded retry after a failed backup. Successful snapshots
can trigger scoped retention of the tagged Compute backup set. Retention keeps
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
