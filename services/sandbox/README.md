# Engineering verification boundary

The Rust verifier has two execution contracts. Go remains responsible for
engineering planning, user-authorized project scope, workspace leases,
source edits, verification receipts, and draft publication.

## Local sandbox

`homelab-engineering-sandbox run WORKSPACE_ID GIT_SHA ACTION` checks a
locally available Git checkout at an exact revision. The operator configures
`HOMELAB_SANDBOX_WORKSPACES` to contain approved 32-character workspace
directories. Bubblewrap isolates each command from host home directories,
secrets, network access and mutable source. The implementation accepts only
fixed, named Go, Cargo, or Nix verification actions, with a bounded timeout.
This entrypoint must run without root authority.

## Kubernetes verifier

`homelab-engineering-sandbox run-contained OWNER/REPOSITORY BASE_REVISION ACTION`
runs inside a worker-managed, non-root Kubernetes Job. The Go worker admits
only a controller-queued `command` operation containing the exact fields
`sandbox_check`, `repository`, `revision`, and `tool`. Requests require
a pinned verifier OCI image, a workspace already owned by the project/worker,
a verified workload node and a project namespace with deny-all network policy.

The Job mounts the existing project workspace volume read-only, has no
mounted service-account token or privileged device, uses restricted process
security settings and has a ten-minute action deadline. The worker owns
Job creation, polling, cleanup and orphan reconciliation.

A GitHub source import in this system uses a tarball snapshot, not a Git
repository. The compute worker writes `.pwm-source-revision` beside the
flattened repository directory `repos/OWNER__REPOSITORY` during the
controller-approved archive import. The verifier checks the exact repository
and base revision in that receipt before and after execution. The receipt
identifies the imported base archive; it is not a cryptographic measurement
of subsequent source edits. Actual modified-file digests are confirmed by
the Go engineering workflow separately.

Five actions are admitted in Kubernetes mode: `go-test`, `go-vet`,
`cargo-test`, `cargo-check`, and `cargo-clippy`. No shell, Git
publication, arbitrary process argument, deployment command or Kubernetes
administrator action is available through the verifier. Source files are
not writable in the verification Job; compiler caches use temporary storage.

An exact `completed` result is recorded only when the native command returns
successfully. A failed or unobservable Job must not be reported as a
successful test. Recovery removes orphaned verification Jobs before
replacement claims are accepted. No API or MCP caller may enable this
boundary or supply its OCI image: these are operator-owned Nix options.

## Image and release

The Kubernetes verifier image is built from `services/sandbox/Dockerfile`
with digest-pinned toolchain base images. Its final reference must be pinned
by OCI digest in the worker's `verificationImage` configuration. The
engineering daemon selects native verification through the independent
`nativeVerification` option. Both options default to disabled.

The image, Go adapter and Rust code require isolated compilation, negative
authorization testing, CNI enforcement checks and end-to-end acceptance
before any operational release.
