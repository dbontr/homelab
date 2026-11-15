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

For registered Kubernetes workers, use the source-owned controller maintenance
tool on Saturn before running the host activation script. It verifies that all
five nodes are Ready and that every controller worker assigned to the target
host is online, drained, unpaused and free of active jobs, workspaces,
unfinished operations and device leases. The `plan` command makes no changes:

```sh
bash services/compute/scripts/worker-host-maintenance.sh plan HOST
bash services/compute/scripts/worker-host-maintenance.sh pause HOST --confirm HOST
```

The `pause` command writes a root-only maintenance receipt, cordons the
physical Kubernetes node, then sets revision-fenced persistent maintenance
state for **every** worker assigned to it. If any step fails, leave the node
cordoned and use the receipt to reconcile controller state; do not run the
host activation script. Keep the receipt through activation, cluster
qualification and any required reboot. Independently check host-native
programs such as EDA hardware servers and virtual machines: controller
drain status covers only controller-owned work.

After the tested candidate has booted and the node, Deployments, worker
registrations and workload execution are healthy, release the maintenance
state from Saturn:

```sh
bash services/compute/scripts/worker-host-maintenance.sh resume HOST --confirm HOST
```

The resume command checks the receipt, exact worker membership and
maintenance revisions, requires the node and Deployments to be healthy,
resumes every worker and only then uncordons the node. An interrupted resume
can be retried with the original receipt. Do not remove that receipt
manually or bypass a failed safety check.

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
The trial changes the running NixOS configuration and explicitly restarts
the selected node's K3s agent so it loads the mirror list. This can interrupt
workloads; complete controller scheduling pauses and independent-workload
clearance before invocation. The trial does not change the selected boot
generation. On failure, the runner restores the previous closure, restarts
K3s again to restore its prior registry configuration, and checks that K3s
and SSH have recovered. The journal is the authoritative trial receipt:

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
