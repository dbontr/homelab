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

Retired controller identities require separate reconciliation. A revoked
identity has `last_seen=0` in the controller record. The host maintenance
tool excludes such an identity only after proving its permanent controller
maintenance pause is active, every resource/lease count is zero and there
is no matching Kubernetes Deployment or Pod. It never automatically unpauses
the retired identity. If an old identity is offline but not paused, the host
preflight **must fail** until an authorized operator investigates and
durably fences it through the controller. A stale heartbeat by itself is
not proof of revocation; do not erase its history or bypass the gate.

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
services, the K3s control plane and persistent media online. Do not
interrupt host-native virtual machines, EDA hardware servers or peripheral
sessions. Defer disruptive host actions until these workloads are idle or
their continuity is independently assured.

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

## Peer image-content acceptance

OCI images loaded directly through `k3s ctr images import` may contain all layer
bytes but lack the `containerd.io/distribution.source.images.dbontr.internal`
content labels that Spegel uses to advertise **individual blobs**. As a
result, a manifest can be fetched from a peer while an uncached compressed
layer fails with `NotFound`. A successful mirror hit alone is insufficient.

The approved host-local helper operates on an **exact, present** image tag
from `images.dbontr.internal`. It walks the amd64 OCI index, manifest,
config and compressed layers. The default verification is read-only:

```sh
bash services/compute/scripts/label-image-content.sh --check \
  images.dbontr.internal/dbontr/compute-gateway:0.1.11
```

For an image deliberately imported through `ctr`, a root operator can
repair missing registry-source metadata from the image's exact repository,
after reviewing the printed missing descriptors:

```sh
bash services/compute/scripts/label-image-content.sh --apply \
  images.dbontr.internal/dbontr/compute-gateway:0.1.11 \
  --confirm images.dbontr.internal/dbontr/compute-gateway:0.1.11
```

The helper fails closed on malformed references, absent descriptors, label
ownership conflicts, concurrent invocations or content SHA-256 mismatches.
It verifies all content **before** modifying any labels. It does not import
an archive, alter image bytes, restart K3s or delete references. Never
overwrite a conflicting label: investigate shared blobs first.

**A label repair alone does not refresh the running Spegel advertisement
index.** Confirm the publisher is workload-idle, pause its controller
workers with `worker-host-maintenance.sh` on Saturn, cordon it, restart
only that publisher's K3s agent, verify readiness, and resume/un-cordon the
workers using the retained maintenance receipt. Do not restart an occupied
compute host or bypass a failed maintenance gate. Then perform the
unprimed cold-layer test below. Favor normal K3s/CRI pulls or source-owned
image publication over future raw `ctr` imports.

Registry advertisement is not proof that a complete image can be fetched.
K3s Spegel hit counters are cumulative, so `--require-hit` may pass because of an
unrelated earlier request. To verify a fresh peer hit, capture the selected
node's baseline before making the test request:

```sh
bash services/compute/scripts/verify-registry-mirror.sh NODE > /run/peer-baseline.jsonl
# Perform one bounded, approved image pull on NODE.
bash services/compute/scripts/verify-registry-mirror.sh \
  --require-hit-since /run/peer-baseline.jsonl NODE
```

Treat an increased counter as evidence of a new peer cache request, not
proof that a particular layer was transferred. To qualify a cold image,
first verify the publisher's OCI index and target-platform manifest are
complete. Before the pull, confirm the consumer has neither the test image
reference nor at least one specified manifest layer digest in its
containerd content store. Record those digests and the baseline metrics.
Then pull the approved image through the configured K3s registry, confirm
the missing layer is present, validate its SHA-256 content and inspect the
consumer's positive hit-counter delta. An imported archive or an image
that already has all its layers cannot establish cold-layer retrieval.

Do not prime the consumer through a one-time manual image transfer before
this acceptance test. Remove only uniquely named test references after
checking that no workload uses them. Keep genuine image references,
working layers, media and rollback generations intact.

## Authenticated offline image recovery

If the K3s peer registry fails to transfer a genuinely uncached compressed OCI
layer, use the **source-owned bounded fallback** on Saturn. This is not a
substitute for the cold-layer acceptance above: it is a recoverable way to
restore an unavailable image from a peer with a known-complete copy.

```sh
bash services/compute/scripts/peer-image-offline-transfer.sh --plan \
  venus janus images.dbontr.internal/dbontr/compute-gateway:0.1.11
```

The plan verifies both enrolled workers are Ready, K3s is active, the
publisher's exact image index is complete, and no Pod references the target
tag. It refuses a destination tag that resolves to any different SHA-256
index. Neither node's image or K3s state changes during planning.

After checking the plan, the operator may explicitly confirm that exact
image for a single approved 600-second transfer:

```sh
bash services/compute/scripts/peer-image-offline-transfer.sh --apply \
  venus janus images.dbontr.internal/dbontr/compute-gateway:0.1.11 \
  --confirm images.dbontr.internal/dbontr/compute-gateway:0.1.11
```

The transfer streams the publisher's OCI export through two strict-host-key,
batch-authenticated worker SSH connections; **no persistent intermediate
archive** is written. The destination may lack the tag or already have the
**identical** image index with missing content, but it may not have a different
index under that tag. On import, the helper verifies that the exact image
index matches, the full image is complete, every descriptor's bytes hash to
their SHA-256 digest, and containerd's repository-source metadata is present.
It preserves shared source aliases. An error never triggers automatic image
deletion or overwriting of an unrelated tag. Partial content after a failed
stream is preserved for inspection and must pass a fresh plan before retry.

The fallback never restarts K3s or bypasses the Compute scheduling authority.
It does not independently establish a fresh Spegel cache hit; an updated
publisher or consumer advertisement still requires the guarded maintenance
and cold-pull verification procedure above. Do not transfer an image currently
used by a running Pod. The dedicated Saturn worker SSH identity is required;
credentials are never placed in scripts, arguments or artifact output.

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
