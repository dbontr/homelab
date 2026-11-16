# Kubernetes batch admission

Kueue v0.20.6 provides admission and quota control for batch Jobs in the `pwm-batch` namespace. K3s owns the versioned Helm release through the `kueue` HelmChart resource in `kube-system`. The Saturn NixOS service reconciles the controller and queue resources after the Kubernetes API is ready.

The `research-cpu` LocalQueue maps to the `pwm-cpu` ClusterQueue. Its ResourceFlavors select physical Venus and Mercury using the standard Kubernetes hostname label, so batch Jobs do not land on Saturn. Conservative admission quotas limit Venus to 1 CPU and 2 GiB, and Mercury to 2 CPUs and 4 GiB. These limits bound Kueue admission, not total Kubernetes node usage; the scheduler still checks physical resource availability.

Queue management is opt-in. The manager handles `batch/job` only and sets `manageJobsWithoutQueueName: false`. Its managed namespace selector requires `homelab.dbontr.me/kueue: enabled`. Unlabeled system Jobs, existing controller Deployments, Project Workspace Pods, and independent hardware leases remain outside Kueue admission. Do not enable Pod or Deployment integrations, or automatic admission for every namespace, without separate migration acceptance.

## Verification

Run `bash scripts/verify-kueue.sh --check` on Saturn for a read-only controller, queue and cluster audit. Run `bash scripts/verify-kueue.sh --smoke` for an isolated, resource-limited Kubernetes Job that checks Kueue admission and cleans up its test Job. Neither mode changes service configuration or controller-maintained Project Workspace state.

New batch Jobs in `pwm-batch` must label their Job metadata with `kueue.x-k8s.io/queue-name: research-cpu` and declare CPU and memory requests. Follow the namespace's restricted Pod Security requirements. Do not apply Kueue labels to Project Workspace-managed Pods or Jobs until both admission systems share a single tested resource-allocation contract.

## Maintenance

Keep the HelmChart manifest and the queue manifest in the homelab repository as the source of truth. Kueue's CRDs and admission webhooks are cluster-scoped; review chart upgrades, keep rollback receipts, and test existing workload creation before promoting new versions.

Do not uninstall the controller while Kueue resources remain. First finish or remove Kueue-managed Jobs, then Workloads, LocalQueues, ClusterQueues, ResourceFlavors and other Kueue resources, according to the official Kueue uninstall procedure. Removing the HelmChart does not justify deleting any Project Workspace PVCs or media.
