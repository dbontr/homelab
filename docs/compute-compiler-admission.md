# Project-scoped compiler image admission

Compute permits a workload image only when the project's worker reports that
exact image as a capability. The source-owned policy in
services/compute/images/project-allowlist.json applies to new workers.
Existing registered workers require an independently authorized change.

Use services/compute/scripts/rollout-project-worker.sh to add an immutable
compiler image. This keeps the worker-agent image, service account,
credentials, namespace, RBAC, quotas, mounts, and network policy unchanged.
The sole admitted Helm manifest difference is PWM_ALLOWED_IMAGES for one
project worker. The existing worker-agent image rollout remains available.

## Authority and prerequisites

An operator must independently approve the source revision, project,
worker release, image digest, and maintenance operation. An active owner
session is not an operation-specific authorization. The operator requires
a configured K3s KUBECONFIG, the reviewed Helm chart and its dependencies.

The script requires the following conditions:

- The Helm release identity, worker, project, namespace and host agree.
- The controller's revision-fenced maintenance endpoint confirms the
  worker is online and drained of jobs, workspaces, unfinished operations
  and device leases.
- All Kubernetes nodes are Ready and the target workload namespace has
  no Pods or Jobs.
- The requested digest belongs to the exact project in the source policy.
- The server-rendered Helm manifest differs only in the worker allowlist.
- No controller Deployment, credential, resource quota, workload image,
  privileged role or network policy is changed.

Mercury is a candidate for the first Go acceptance, not a reservation.
Inspect its workloads immediately before applying.

## Plan and protected execution

From an independently reviewed and approved source checkout:

    export PWM_HELM_CHART="$PWD/services/compute/deploy/helm/project-workspace"
    export KUBECONFIG=/etc/rancher/k3s/k3s.yaml

    release=pwm-project-fe67735ab7b7-mercury
    image=ghcr.io/dbontr/homelab/engineering-go@sha256:f33edc74b06fa39246ec30a7b3bf13d48b97d7590424d4ecd7aa6882d5b0558e

    bash services/compute/scripts/rollout-project-worker.sh \
      plan "$release" --allow-image "$image"

    # Only the independently authorized operator executes this action.
    bash services/compute/scripts/rollout-project-worker.sh \
      apply "$release" --allow-image "$image" --confirm "$release"

The plan does not change Kubernetes resources. It validates the current
Helm release, controller admission state and server-side Helm dry run.
Apply pauses admission through the revision-fenced controller endpoint
and changes only the worker's image allowlist.

After the worker Deployment is Ready, the controller must record a fresh
heartbeat with the exact digest. Before resuming, a temporary restricted
Pod in the existing deny-all workload namespace must demonstrate the
pinned compiler version, a Kubernetes pull event, the container image
digest and successful execution. It has no service account token,
host mounts, additional privileges or writable container root.
Its deletion must be confirmed.

The same procedure applies to the approved immutable Axiom compiler
image on a dedicated axiom-engine worker, under a separate scoped approval.

## Failure and recovery

A failure before the controller pause leaves the worker unchanged.
After the pause, any uncertain controller response requires explicit
reconciliation. A failed pull or stale heartbeat causes a Helm rollback
to the captured revision. The worker is deliberately left paused
until the operator verifies the Deployment, image policy, Pod cleanup,
maintenance revision and absence of active work. Do not retry blindly.

To revoke an image after a successful rollout, use another independently
authorized, drained, revision-fenced maintenance operation. Do not edit
a running Deployment or overwrite controller capabilities directly.

Compiler admission does not provision a signed non-admin Engineering
principal, editor grant or protected token, and does not deploy the Go
Engineering daemon. The first-use acceptance still requires a pinned
repository checkout, a successful Compute build/test, durable logs,
a reproducible source revision and a linked Projects artifact.
