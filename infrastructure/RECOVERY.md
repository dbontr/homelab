# Saturn recovery

## Source of truth

Saturn is defined by the pinned NixOS flake in this repository. The
configuration entrypoint is `nixosConfigurations.saturn`, selected as
`#saturn` by NixOS install and rebuild commands. Publish this repository
privately under the personal `dbontr` account; do not use company credentials.

Git preserves declared configuration, not running application state. Keep
unencrypted credentials, Kubernetes cluster identity, databases, Docker
volumes, project files, and media outside this repository.

## Recover a replacement machine

1. Boot a trusted NixOS installer and establish network access. Identify
   storage devices before repartitioning; avoid changing disks containing
   the separately retained application data.
2. Mount the new system at `/mnt`. Clone the approved private repository
   into `/mnt/etc/nixos` using a temporary read-only Git credential.
3. Inspect the replacement hardware and regenerate
   `hosts/saturn/hardware-configuration.nix` for its disk identifiers and boot
   configuration. Review changes before proceeding.
4. Restore required out-of-band secrets and identity material through
   the intended protected paths, not through Git. In particular, the
   Cloudflare tunnel identity, K3s server token and datastore, VPN keys,
   mail identity, and Kubernetes Secrets are not recreated by a flake.
5. Install from the checked-out source with
   `nixos-install --flake /mnt/etc/nixos#saturn`, after confirming that
   the target filesystem and required paths are correct.
6. Restore mutable application volumes and K3s state from separate
   verified data backups. Images may also be rebuilt from Dockerfiles
   and imported into the K3s image store.
7. Verify networking, K3s, storage mounts, tunnel authentication,
   application health and ingress policy before admitting workers.

Confirm admission labels, immutable image references, persistent volumes and network-policy enforcement before admitting Project Workspace workers or accepting jobs.

## Selected-state recovery

The selected encrypted recovery contract and its activation gates are in [Selected Recovery](docs/plans/Selective%20Recovery.md). The new service is opt-in. Existing backups and encrypted Janus replication remain in place until a verified restore demonstrates that the selected state and protected bootstrap identities suffice.

## Flake archive contents

The selected-state archive contains a full Git bundle, `flake-working-tree.tar`, and
`flake-status.txt`. The bundle preserves committed history and refs; the working-tree
archive preserves tracked files and untracked files that are not Git-ignored, including
uncommitted changes. It intentionally excludes Git-ignored paths, which may contain
secrets. Restore the bundle and compare the recorded checkout status before applying
the working-tree snapshot. Preserve the original archive until the reconstructed
configuration has been verified.

A successfully created local archive does not establish disaster recovery. Confirm
the encrypted off-host copy can be decrypted and restored independently, including
K3s identity, PostgreSQL, learning attachments, Project Workspace controller state,
and artifact bytes, before decommissioning existing backups. The selected backup
module remains disabled until those dependencies and recovery tests are complete.
