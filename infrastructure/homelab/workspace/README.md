# Workspace worker site profiles

The four `worker-<host>.yaml` files define placement, quotas, worker identities,
and permitted workload images for the K3s compute hosts. The canonical Helm
chart and Compute binaries belong to this repository under
`services/compute/deploy/helm/project-workspace` and `services/compute`.
The worker OCI image is pinned to a registry digest. No credentials belong in
these files.

These profiles are source inputs, not a record of the currently installed Helm
values. The running base-worker releases are named
`pwm-workspace-jupiter`, `pwm-workspace-venus`,
`pwm-workspace-mercury`, and `pwm-workspace-janus`.
Existing releases retain controller-issued worker identities and Kubernetes
credential Secrets. Before using a profile in an upgrade, compare its
`worker.name`, `worker.instanceID`, workload namespace, node selector,
Secret references, quotas, and hardware settings with the live release. Do
not overwrite live identities merely to make a profile match a template.

## Read-only preflight

Run these commands from the authorized Kubernetes operator host:

```sh
for host in jupiter venus mercury janus; do
  release="pwm-workspace-$host"
  helm --namespace pwm-system status "$release"
  helm --namespace pwm-system get values "$release" --all
done
kubectl get nodes
kubectl --namespace pwm-system get deployments -o wide
```

Confirm the intended image is retrievable on each host, that the live worker
Deployment and controller heartbeat are healthy, and that no project job or
workspace is active before a worker release is upgraded.
To inventory image drift without triggering reconciliation or restarting Pods:

```sh
target='ghcr.io/dbontr/homelab/worker@sha256:c7ab035296db9aec360611c52737ca4e94143fb959c7ad56ca536cc20fd6ee4f'
kubectl --namespace pwm-system get deployments -o json |
  jq -r --arg target "$target" '
.items[] | . as $deployment |
(.spec.template.spec.containers[] | select(.name == "worker") | .image) as $image |
[($deployment.metadata.annotations["meta.helm.sh/release-name"] // "unmanaged"),
 $deployment.metadata.name,
 (if $image == $target then "pinned" else "drift" end),
 $image] | @tsv
'
```

A `drift` entry is a maintenance candidate, not authorization to upgrade.
The Deployment metadata does not carry the Pod's `app=pwm-worker` label;
inventory releases by their Helm ownership annotations instead.

This Kubernetes-only inventory does not establish controller queue emptiness,
worker-heartbeat freshness, or host registry configuration. Those require
separate admission checks. The controller's job registry and worker
heartbeat must be inspected separately: a Ready Pod alone does not prove
the worker is accepting jobs.

For an approved single-worker upgrade, render the proposed source-owned Helm
chart against the exact live values first. Always set
`controller.enabled=false` and `worker.enabled=true` explicitly. Helm
3.20 or later supports `--reset-then-reuse-values` to combine chart
defaults with existing release overrides; inspect the rendered Deployment,
namespace/RBAC objects, Secret references, image digest, and absence of
controller resources. Then use `--atomic --wait` with a retained Helm revision,
checking readiness, fresh controller heartbeat, and real workspace execution
afterward. Do not upgrade all four workers in one operation.

The project-worker reconciler provisions only missing project/host pairs; it
does not replace registered workers, even if their live image is older. The
remaining project-worker image migration must be separately admitted and
performed one release at a time, preserving project credentials and work.

## Enrollment and accelerator custody

New worker enrollment uses a distinct, one-use controller pairing code and a
separate `pwm-worker-<host>-credentials` Secret containing the
`worker_token` key in `pwm-system`. Do not commit, print, or distribute
these credentials. Reuse the live Secret for an existing worker rather than
pairing it again.

Jupiter's `jupiter-devices.json` declares the operator-approved accelerator
inventory. Recreate its ConfigMap only after verifying actual Kubernetes
`status.allocatable` resources and that no legacy worker still advertises
the same physical devices:

```sh
kubectl -n pwm-system create configmap pwm-accelerator-inventory \
  --from-file=devices.json=infrastructure/homelab/workspace/jupiter-devices.json \
  --dry-run=client -o yaml
```

The command above previews the manifest only. Apply it through the approved
release process after independent device and lease checks. The source
declaration alone does not establish hardware health.

Compute-host K3s registry configuration is managed through each host's own
NixOS release and rollback path, not through worker Helm values. Do not use
privileged Kubernetes debug Pods to bypass a host's SSH or security boundary.
Keep the existing controller state, Helm revisions, NixOS rollback generations
and media mounts intact throughout acceptance.
