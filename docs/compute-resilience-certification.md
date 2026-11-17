# Compute resilience acceptance

## Scope and isolation

The controller owns durable project identity, batch Job requests, terminal records, and hardware leases. Kueue alone owns CPU quota admission. Neither Kueue nor batch reconciliation may take a workspace FPGA, GPU, NPU, or EDA device reservation. Jupiter and Janus are excluded from the `research-cpu` resource flavors; the CPU batch queue targets Venus and Mercury. Production reconciliation and release execution are outside this acceptance suite.

Before using a real Kubernetes API, run the read-only qualification preflight with an explicit kubeconfig for a separate, three-node k3d cluster. An unsupported environment returns exit code 77; a context that does not match the requested disposable cluster is refused. This check is not a substitute for a genuine controller/worker failure or cold peer-transfer receipt.

```sh
KUBECONFIG=/absolute/path/to/disposable-k3d.yaml \
  bash services/compute/scripts/tests/disposable-k3s-preflight.sh compute-qualification-<unique-id>
```

Run the local fault-injection suite from an isolated checkout:

```sh
bash services/compute/scripts/tests/compute-resilience-isolated.sh
```

For repeated race detection on the same disposable fixtures, run:

```sh
COMPUTE_RESILIENCE_REPEATS=10 \
  bash services/compute/scripts/tests/compute-resilience-isolated.sh
```

The runner permits one to ten repetitions and enables Go race detection with bounded parallelism when repeated. A toolchain mismatch returns exit code 77, not a passing receipt. The replay fence prevents repeated Kubernetes Job creation attempts; it does not promise exactly-once arbitrary effects within a container after worker/network failures.

The suite uses temporary SQLite files, fake Kubernetes HTTP servers, and SSH/containerd mocks. It does not run `kubectl`, call the live controller, alter Kueue queues, or transfer production OCI content. A Go toolchain compatible with `services/compute/go.mod`, SQLite development headers, and `jq` are required. Do not interpret a simulated pass as evidence of a successful live outage or genuine cold peer distribution.

## Durable execution invariants

1. A project-scoped request ID binds one exact command, image digest, CPU and memory allocation to a controller-owned Job ID.
2. A transaction commits `create_attempted=1` before an outbound Kubernetes Job creation call. A lost response, process termination, or 409 conflict cannot authorize an unguarded second creation.
3. Reconciliation uses the exact remote Job name, project and request annotations, immutable command/image/resources and its Kubernetes UID. A different UID or execution specification is rejected.
4. An absent Job after a recorded creation attempt is `recovery_required`, never silently retried or called a failed execution. This also applies when a completed or failed Job's Pod output is temporarily unavailable. The controller can resume the original Job and capture its log output when it reappears.
5. During migration, existing requests without a creation-attempt field are conservatively treated as already attempted. Recover or cancel them only after inspecting their original remote execution.
6. Cancellation is issued with a Job UID precondition and foreground deletion. Do not reuse the request ID for different work. A vanished workload whose prior attempt is recorded may have run; the cancellation result retains this uncertainty.
7. Device lease recovery is independent of Kueue admission. A failed native worker retains its device reservation until the documented cleanup-verification and confirmation sequence completes.

The `recovery_required` record is an explicit operator decision point. Inspect the original Job UID, Kubernetes audit history, associated Pod events and results before any change. Never delete or recreate the original Job merely to clear this state. The proposed branch adds no endpoint to override the safety fence.

## OCI peer recovery

For a future independently approved, controlled test, reserve a nonproduction image digest and an eligible receiver with at least one genuinely absent Linux/amd64 layer. Run the source-owned cold-pull verifier only after the production freeze is lifted and the integration coordinator approves the receiver and image:

```sh
bash services/compute/scripts/verify-cold-peer-pull.sh DONOR RECEIVER IMAGE SHA256_DIGEST
```

It checks complete donor content, a missing receiver layer, the final receiver index digest and CRI visibility, and a **new receiver-node** Spegel cache hit. A donor-only hit, a cached receiver, or an incomplete transfer is a failure. This live test is not performed by the isolated suite.

Upstream [Spegel v0.8.0](https://spegel.dev/blog/spegel-v0.8.0/) (October 2026) reworked advertisement of locally imported containerd images, but it requires containerd 2.2+ and replaces `spegel_advertised_image_digests` with `spegel_advertised_keys`. The source-owned mirror receipt currently requires the older registry-labelled metric. Before any future embedded-Spegel upgrade, confirm the exact K3s/Spegel/containerd versions and update metric parsing without weakening the registry-specific and receiver-specific cold-pull proof. An upstream release announcement is not evidence that this K3s deployment already includes the fix.

If a managed peer cannot serve a complete layer, use the separately authorized, digest-verified `recover-oci-peer.sh` only on a permitted idle receiver. That path rejects unrecognized hosts and conflicting receiver digests, bounds SSH sessions, streams peer content without archives, and verifies containerd source metadata. A successful fallback does **not** certify Spegel cold-layer service. No script deletes production images or forces K3s restarts.

## Coordinator gates

Before merger or release, collect passing test output and verify the exact source revision. Review the controller schema migration with the Backup workstream and the controller's reconciliation/receipt changes with the Projects workstream. A controlled failure-injection run must separately demonstrate restart/recovery and quota handling without touching active research devices, followed by a genuine cold-layer Spegel receipt. Check existing five-node and deployment readiness before and after any separately authorized release. Do not merge, rebuild, or activate this branch as part of testing.
