# Axiom Engine build authorization

## Scope

The independent privileged executor admits bounded Axiom Engine CPU batch work only under its exact project identity after a reviewed policy and owner-approved NixOS release are activated. Privileged batch jobs use the restricted Kueue queue in the `pwm-batch` namespace, not Axiom Engine's dedicated Workspace workers. Existing project worker identities, namespaces, quotas and NetworkPolicies remain unchanged.

Allowing a project in the executor does not provision a general editing workspace, install toolchains, enable network access, or grant new deployment permissions.

## Build dependencies

Axiom Engine requires:
- Rust 1.91.1, Cargo, rustfmt and clippy;
- wasm32-unknown-unknown Rust standard library;
- pinned Lean 4.34.0 and Lake;
- offline-vendored Rust crates for its Cargo workspace;
- the repository at an immutable reviewed commit;
- an approved OCI build image with all dependencies preinstalled.

The existing Debian Bookworm image is not an Axiom Engine toolchain image. It is intentionally inadequate for Rust, Lean or WebAssembly build certification. Build and publish a separate image through the approved image-builder path with restricted egress. Verify its provenance, contents and digest before registering its exact OCI digest in the independent operator image policy. Do not relax any Workspace or batch-namespace NetworkPolicy for dependency installation.

## Protected activation

1. Review the policy change, verify its precise project scope and independent executor tests, and record the exact source commit.
2. Validate the generated NixOS source and evaluate the exact Go privileged-executor policy using disposable or nonproduction tooling.
3. Stage the candidate through the independently authorized NixOS release interface. Do not activate a release that would disrupt running Compute or Projects jobs.
4. Obtain separate owner approval for activation and retain the rollback generation.
5. After successful activation, inspect the active project's policy and submit a minimal, owner-authorized and resource-bounded diagnostic CPU job to the restricted Kueue queue after confirming current research-job admission capacity.
6. Register the independently reviewed build image by exact digest; submit bounded build/test work without mounting controller credentials or altering production volumes.

## Evidence

The accepted build must report exact source revision, image digest, rustc, cargo and Lean versions, compiler and test return codes, and source-verified Lean proof receipts. A queued job, an incomplete run or a stale status does not qualify the build.

This policy never gives Axiom Engine permission to modify other research projects, rewrite the production project catalog, restart controllers, or change other workspace reservations.
