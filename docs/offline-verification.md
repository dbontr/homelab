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

Runs the checks for the imported Compute, Learning and Health Go modules using their declared toolchain versions, evaluates all enrolled NixOS configurations, and builds the platform package without activating it. Required Go compilers, Nix inputs and module dependencies must already be cached.

A successful core check is not evidence that all modules build. Successful Nix evaluation does not establish operational readiness or hardware access.

## Acceptance expectations

- Inventory: test dynamic enrolled and SSH-mode nodes; reject invalid identity, wrong node roles, unauthorized managed device leases and malformed inventory.
- Profiles: create and reload versioned build configurations; reject stale revisions, path traversal and credential-like parameter names.
- Interfaces: compare native HTTP and MCP execution against the same local and mock federated capabilities, including authorization denials.
- Privilege: reject changed action, target, arguments and replay, including after a simulated restart; require the executor to register its handlers. Verify passphrase strength, bounded guesses, owner socket access and operation allowlists.
- Accounts: validate allowlisted OAuth providers, PKCE and one-time callback state without displaying authorization codes, private verifiers or access tokens.
- Failure: test unavailable integration backends and report errors rather than unverified success. Bound the size and duration of SSH inspection output without installing anything on the remote host.

Build profiles are saved definitions, not permission to run a command. The security package provides reusable authorization contracts but no configured production credential broker or cluster executor. The credential library supplies encrypted, owner-only file storage and mock-tested OAuth token exchange. A local owner passphrase verifier and owner-only Unix-socket grant issuer are available for an independent operator process, with mock-only acceptance tests. Operational integration still requires an installed privileged executor process, secure owner-side UI, protected IPC between the user interface and executor, hardware-identity verification at execution, and independent release approval.
