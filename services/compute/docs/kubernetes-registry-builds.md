# Kubernetes registry-backed workspace builds

Workspace environments publish OCI images to an operator-approved HTTPS registry. The controller defines the environment and checks its version. A project-scoped worker starts a rootless BuildKit Job in its own Kubernetes namespace, using the existing workspace claim as a read-only build context. Builder credentials reside only in a separate namespace Secret. No worker receives a host Docker socket or a cluster-administrator token.

## Registry prerequisites

1. Select a registry/repository prefix owned by the operator, such as `ghcr.io/dbontr`, with TLS and restricted write access. Reserve `pwm-env-<project>-<environment>:v<version>` images for Workspace. Do not reuse the controller's GitHub repository PAT; provision an independent token that has only the registry package permissions required.
2. Mirror or select a verified immutable `moby/buildkit:rootless@sha256:<64-hex-digest>` image. Verify the digest and the image's platform independently before admitting it. Tag-only names and unverified example digests are not accepted.
3. For each opted-in worker project namespace, create a `kubernetes.io/dockerconfigjson` Secret named by `worker.builder.authSecretName`. It must contain a `config.json` under `.dockerconfigjson` with permission to push only to the approved prefix. Provision a separate pull Secret if the registry requires authentication. Do not put either Secret in Git.
4. Set controller `environmentRegistry` to the same prefix. Set worker `builder.image`, `builder.registry`, and `builder.authSecretName`; set `workspaceImagePullSecret` when needed. Add the approved base image to that worker's `allowedImages`, e.g. `golang:1.27.3-bookworm`. These values are opt-in; without them Kubernetes builds remain disabled.
5. Apply the Workspace MCP release through its independently authorized Helm/NixOS deployment process. Check namespace credentials, readiness, image pull permissions, and registry connectivity before invoking a build. Runtime platform releases and application deployments remain separate.

The Helm worker chart retains the namespace's default-deny workload NetworkPolicy. A narrower additional policy allows only Job Pods bearing `pwm.dev/kind=image-builder` to resolve DNS and reach TCP/443 and TCP/80 for registry and package downloads. **The registry credential Secret must be scoped to the worker namespace.** The build Job mounts that Secret read-only, disables automatic service-account credential mounts, and runs without privilege escalation or host device access. The destination tag and base image are checked against controller and worker policy. Exporting or loading Docker daemon tar archives is deliberately unsupported for registry-backed Kubernetes images.

## Go, Rust and WASM toolchain

`deploy/kubernetes/Dockerfile.go-rust-wasm` describes a Debian Bookworm image with:

- Go 1.27.3 from the official Go image;
- Rust 1.99.1, Cargo, Rustfmt and Clippy from the official Rust distribution;
- `wasm32-unknown-unknown` installed through rustup;
- Dioxus CLI 0.7.10, build-essential, Clang, Git, pkg-config and OpenSSL development headers.

The Dockerfile expects official registry access for `golang:1.27.3-bookworm` and `rust:1.99.1-bookworm`, plus the package and Rust registries. The manually dispatched `.github/workflows/go-rust-wasm-image.yml` workflow builds this Dockerfile and publishes `ghcr.io/dbontr/pwm-go-rust-wasm:go1.27.3-rust1.99.1` using GitHub's workflow-scoped package token. It reports the immutable image digest. The workflow must exist on the default branch and requires available GitHub Actions capacity; it does not deploy the image or configure worker credentials. An operator may also build the image in the approved Workspace registry builder once that service is deployed. An environment definition must use the exact approved Go base in its `FROM` instruction.

Validate the completed image in an isolated project workspace:

```sh
go version
rustc --version
cargo --version
rustup target list --installed
dx --version
cd services/core && go test ./... && go vet ./...
cd ../ui/console && cargo check --target wasm32-unknown-unknown
```

For the complete monorepo, use the `go.work` toolchain version (currently Go 1.27.3) and run the other Go modules' tests separately where needed.

## Self-hosted validation

The repository provides `scripts/verify-registry-builder.sh` for local validation without GitHub Actions. The host must have Go 1.27.3 or newer, a C compiler, `pkg-config` with SQLite development files, Helm, Docker Buildx, and network access to resolve the Dockerfile's public base-image metadata. Nix is optional for syntax validation.

```sh
bash scripts/verify-registry-builder.sh
```

The script runs the complete Go test suite and vet, renders and lints the builder-enabled Helm chart, checks that mutable builder tags are rejected, checks the toolchain Dockerfile with Buildx, and parses the NixOS module when Nix is available. It does not deploy workloads, publish registry images, or run live Kubernetes integration tests.

## Acceptance boundaries

An environment definition or successful registry push is not proof that a new workspace can use the image. Confirm that the worker reports an approved base image; that the build completes with a stable `sha256:` digest; that the resulting registry image can be pulled into a separate nonprivileged workspace; and that a full Go and Rust/WASM build passes. Verify failures when the registry credentials are missing, a Dockerfile uses an unapproved base, the destination prefix differs, or the builder Pod requests a privileged security context.

The builder's Kubernetes admission and operation under the cluster's `restricted` Pod Security policy must be tested before enabling it on production worker namespaces. Image-build operations can consume significant CPU, storage and outbound bandwidth; run them within quotas and with separate credentials.
