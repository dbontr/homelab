# Workspace MCP deployment site values

These four files contain only homelab-specific node placement, quotas, approved image choices and Kubernetes object names. Worker logic, Helm templates, Go binaries, and the distribution process belong to [dbontr/workspace-mcp](https://github.com/dbontr/workspace-mcp).

Source revision: `9967ebc7c96393bc92415869260c746f3bfa239c`, which added node-scoped worker scheduling in PR #72. A SHA-256-checked offline controller/worker/chart/source bundle was built from this exact revision on Saturn. Registry-published, digest-pinned OCI images remain preferable before switching to the NixOS module's managed release deployment.

The files deliberately set `controller.enabled: false`. Saturn's existing controller and its persistent volume are authoritative and must not be duplicated or replaced. Each worker runs on and schedules workloads to its own named compute host, uses a separate workload namespace and a separate pre-provisioned worker credential Secret, and carries only project-scoped Kubernetes permissions. The worker Secret is not created here.

The source image tag is valid only after the release archive has been imported to that worker's containerd image store; `IfNotPresent` prevents accidental remote pulls. For new nodes, verify the release `SHA256SUMS` and image archive before import. This tag is not an independently published registry digest and should not be mistaken for one.

## Enrollment and activation

An authorized administrator must issue **fresh**, one-use pairing codes through the controller, pair each worker with ID `pwm-jupiter`, `pwm-venus`, `pwm-mercury` or `pwm-janus`, then provision a distinct `pwm-worker-<node>-credentials` Secret in `pwm-system` containing a `worker_token` key. Do not commit, copy into chat, or log worker tokens. A temporary Venus test credential was revoked; reuse is prohibited. Before enabling the Jupiter GPU/NPU advertisement, retire that same device inventory from the legacy `saturn-acceptance` worker so that scheduling cannot double-count the physical accelerators.

With the operator-owned secrets available, install independently using the **same checked source revision's** `deploy/helm/project-workspace` chart and each `worker-<node>.yaml`:

```sh
helm upgrade --install pwm-ws-jupiter deploy/helm/project-workspace --namespace pwm-system --values homelab/workspace/worker-jupiter.yaml --wait
helm upgrade --install pwm-ws-venus deploy/helm/project-workspace --namespace pwm-system --values homelab/workspace/worker-venus.yaml --wait
helm upgrade --install pwm-ws-mercury deploy/helm/project-workspace --namespace pwm-system --values homelab/workspace/worker-mercury.yaml --wait
helm upgrade --install pwm-ws-janus deploy/helm/project-workspace --namespace pwm-system --values homelab/workspace/worker-janus.yaml --wait
```

The Helm chart location above is relative to the **Workspace MCP repository** checkout; values files are relative to the **homelab repository** checkout, so supply absolute paths or adjust the commands when the repositories are separate. Verify live controller heartbeats, per-node Pod scheduling, completion/recovery, authorized hardware leases and exact image IDs before declaring service operational. These manifests are preparation, not evidence of deployment.

Do not enable `services.workspaceMcp` in the NixOS host until matching reachable `@sha256:` controller/worker registry references are available and the source flake lock is updated. The module deliberately requires image digests, and it must preserve Saturn's existing state PVC.

## Accelerator inventory

Jupiter's operator-approved `jupiter-devices.json` describes the two verified K3s extended resources. Recreate the nonsecret inventory ConfigMap only after checking the node's actual `status.allocatable` resources and confirming that the legacy acceptance worker no longer advertises these same physical devices:

```sh
kubectl -n pwm-system create configmap pwm-accelerator-inventory \
  --from-file=devices.json=homelab/workspace/jupiter-devices.json \
  --dry-run=client -o yaml | kubectl apply -f -
```

The ConfigMap is mounted by the Jupiter worker release; the file provides declared inventory, not automatic discovery or a guarantee of device health. Helm's project-scoped worker and the Kubernetes scheduler enforce the resource allocation.
