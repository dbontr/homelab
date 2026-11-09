# Offline source verification

All checks operate on local source and test doubles. They do not activate NixOS, contact remote SSH equipment, enroll compute workers, or change a running service.

## Core

```sh
./scripts/verify-offline.sh --core-only
```

Runs tests, race detection and vet in the Go platform with network module downloads disabled. Tests use temporary state directories and local HTTP test servers. The core tests do not require live Compute, Learning, Health, Seerr or Proton services.

## Full monorepo

```sh
./scripts/verify-offline.sh --all
```

Runs Go tests, race detection, vet and builds for all four Go modules; Rust formatting, offline tests and WebAssembly type checks; Helm lint and render safety checks; Nix flake evaluation for all enrolled hosts; and local builds of the platform, engineering, security, executor and fleet packages. It also executes the standalone release-integrity regression suite. Required Go compilers, Nix inputs and module dependencies must already be cached.

A successful core check is not evidence that all modules build. Successful Nix evaluation does not establish operational readiness or hardware access.

## Acceptance expectations

- Inventory: test dynamic enrolled and SSH-mode nodes; reject invalid identity, wrong node roles, unauthorized managed device leases and malformed inventory.
- Profiles: create and reload versioned build configurations; reject stale revisions, path traversal and credential-like parameter names.
- Interfaces: compare native HTTP and MCP execution against the same local and mock federated capabilities, including authorization denials.
- Privilege: reject changed action, target, arguments and replay, including after a simulated restart; require the executor to register its handlers. Verify passphrase strength, bounded guesses, owner socket access and operation allowlists.
- Accounts: validate allowlisted OAuth providers, PKCE and one-time callback state without displaying authorization codes, private verifiers or access tokens.
- Failure: test unavailable integration backends and report errors rather than unverified success. Bound the size and duration of SSH inspection output without installing anything on the remote host.

Build profiles are saved definitions, not permission to run a command. The repository includes an independent owner security service, protected credential vault and OAuth broker, bounded privilege executor, fleet release coordinator, and owner-authenticated administrative interface. These source implementations require the verification above and explicit deployment configuration before they can be accepted as operational controls. The verifier does not connect to real vendor accounts, validate hardware identity on running hosts, establish uninterrupted media availability, or certify production recovery. Those require separately recorded live acceptance.
