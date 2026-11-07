# Engineering standards

The repository is the authoritative source for NixOS infrastructure and all custom Go service implementations. It should resemble a deliberately designed product, not a collection of conversation transcripts or historical migration fragments.

## Ownership and layout

- `flake.nix`, `flake.lock`: pinned, authoritative fleet composition.
- `infrastructure/`: machine topology, router and firewall policy, media deployments, K3s and diagnostics. Runtime credentials and media bytes are not stored here.
- `services/`: separately buildable Go implementations that execute work, teach, and manage health records. Service-specific deployment definitions stay with their implementation.
- `platform/`: the single personal operating model, business operations, native HTTP API, MCP gateway, media integration, and approval coordination.
- `packages/`, `nixosModules/`: reproducible packaging and service activation.
- `docs/`: concise architectural contracts, current operational procedures and authoritative references.

A project is not a repository. A project may have several Git remotes, goals, artifacts, shared libraries and tasks, with stable identities independent of names. Do not duplicate source-of-truth records across services merely to improve presentation. Cross-system operations use explicit interfaces and stable references.

## Change discipline

1. Find the canonical owner of the behavior before editing.
2. Prefer native Go and Nix primitives. Use shell only for necessary system operations, and JavaScript only for deliberate user-interface functionality.
3. Update the owning implementation and its tests. Do not introduce parallel legacy and replacement paths unless a real runtime migration requires both.
4. Keep service-level resource access least-privileged and define every cross-system operation at a meaningful permission boundary.
5. Use an independent build and release pipeline: source validation, unit/integration tests, Nix evaluation, staging, safe activation, health verification and rollback.
6. Keep versioned application state outside the repository. Preserve media and all unique runtime data through deployments.
7. Maintain the root REFERENCES.md with primary specifications that materially govern the current code. Use external references only as needed.
8. Exclude logs, local caches, temporary images, private configuration, credentials, chat artifacts, generated assistant prompts, duplicate readmes and obsolete install scripts.
9. Prefer small, direct implementations. Every additional service, abstraction, and persistent store must have an operational purpose.
10. Never mark a migration complete until actual functional acceptance passes. A successful Git commit or Nix evaluation does not demonstrate a running service.

## Verification

From `platform/`, run `go test ./...`, `go test -race ./...`, `go vet ./...` and the relevant module builds. From the repository root, evaluate the five NixOS configurations and exercise the actual MCP and service APIs. Use real permission-negative tests, representative media and compute workflows, and service rollback checks before activating a new generation.

The repository should remain comprehensible as a whole: common naming, coherent resource identifiers, one published user-facing contract, and independent fault isolation without conceptual fragmentation.
