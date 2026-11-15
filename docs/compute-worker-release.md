# Compute worker release

The four compute workers are independent NixOS hosts. Release validation and
live activation are separate operations. A worker trial can interrupt its
K3s agent and node-local workloads; Kubernetes Job inventory alone does not
establish that Project Workspace hardware leases or sessions are idle.

## Source and build

Use a clean checkout at the exact approved Git commit. Verify the checkout
has no modified files, and preserve the source commit ID with the release
receipt. Stage the same source and `flake.lock` on every worker. Build on
each worker without switching its active NixOS generation:

```sh
nix build --no-link --print-out-paths --no-write-lock-file \
  /srv/homelab-staged/COMMIT#nixosConfigurations.HOST.config.system.build.toplevel
```

Replace `COMMIT` and `HOST` with the staged source directory and the
worker's registered NixOS hostname. Record the resulting immutable Nix store
path. Check that its `etc/rancher/k3s/registries.yaml` includes the declared
`images.dbontr.internal` and `ghcr.io` mirrors. Compare the candidate
`etc` tree with `/run/current-system/etc`; explain every difference,
especially credentials, PAM, SSH, systemd, D-Bus and network configuration.

## Maintenance gate

Before **each** worker trial, use the authorized Compute controller interface
to inspect active projects, execution sessions, hardware leases and pending
work. Establish and record maintenance clearance, including the consequence
of stopping that node's K3s agent. Check Kubernetes node readiness, Pod
placement and Jobs, independently verify SSH access and the boot generation,
and retain console recovery access. Do not proceed when controller lease
state is inaccessible or maintenance clearance is ambiguous.

Never activate multiple workers concurrently. Keep Saturn, its router
services, the K3s control plane and persistent media online. Jupiter may run
independent GPU/EDA workloads; confirm those separately. Hold Mercury until
its broader NixOS configuration drift has been reconciled.

## Reversible host trial

On an approved worker, run the checkout's
`scripts/worker-detached-activation.sh` as root with the explicit
`--maintenance-approved` flag and its built candidate NixOS closure:

```sh
bash scripts/worker-detached-activation.sh --maintenance-approved \
  /nix/store/CANDIDATE-nixos-system-HOST-VERSION
```

The script checks the host identity, current boot profile, candidate store
closure, intended registry mirrors, and initial K3s/SSH service status.
It launches an independent `systemd-run` trial with a host-local lock.
The trial does not change the boot generation. On failure, the runner
attempts to restore the previous closure and checks that K3s and SSH have
recovered. The journal is the authoritative trial receipt:

```sh
journalctl -u 'homelab-worker-trial-*' --no-pager
```

A reported successful host trial is **not** complete cluster acceptance.
From Saturn, verify that the selected node rejoins Ready, that project
workers are Ready and registered, and that registry advertisement and
digest-qualified image pulls still work. Use the existing
`services/compute/scripts/verify-registry-mirror.sh` and
`verify-cluster-image-pulls.sh` contracts. Verify an actual Project
Workspace execution and lease release on the node. Confirm failure-recovery
behavior before making the boot selection persistent.

## Persistent selection and rollback

Only after the independent post-trial checks pass should the operator
promote the exact tested candidate into the selected boot profile using
NixOS's documented generation mechanism. Preserve the previous generation,
the previous closure path, the source commit and the activation journal.
After a controlled reboot, repeat K3s, registry, worker, hardware and
application checks. Roll back the selected generation through independent
host management if any gate fails.

Do not delete old image caches, host generations, state directories or
credentials as part of this procedure. A source build, a live registry
advertisement, or a successful `switch-to-configuration test` alone cannot
establish workload recovery.
