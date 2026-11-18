# Go platform and React console qualification

## Source ownership

The runtime control plane uses Go for MCP/API endpoints, the independent security authority, privileged executor, Compute, Engineering and service orchestration. The web client uses React, TypeScript, Tailwind CSS and source-owned shadcn/ui primitives from `ui/web`. The browser does not possess administrator credentials.

The operating-system privilege and trust boundaries are unaffected by the choice of programming language. Security and executor remain independent units. The Go migration is configured in `infrastructure/homelab/security-authority.nix`, including the Go-only typed actuator and an operator-provisioned migration receipt path.

## Safety and deployment boundaries

The source selector is not a live cutover. The active Rust authority is the sole writer of its private authenticator, TOTP replay record, encrypted vault and session state until the separately approved transition.

A protected cutover must:

1. Verify a clean, exact source checkout and successful Go security/executor regression suites, including forbidden identities, expired TOTP, single-use destructive confirmation, state replay, audit truncation, actor/host targeting and uncertain-outcome reconciliation.
2. Qualify the Go authority with a detached copy of the **current** Rust-written state, including every encrypted credential, using the existing `security-migrate` and `homelab-security-preflight` contracts. Do not restore an old snapshot after password changes, revocation or TOTP consumption.
3. Obtain the new Go migration receipt with the current selection-marker fingerprint, approved exact source, owner authorization and independent release identity. Provision it at the protected `authority-go-cutover.json` location through credential custody, not Nix source or a chat message.
4. Confirm that the Go executor's exact operation allowlists, typed systemctl/Kubernetes/release handlers, exclusive audit journal, and irreversible-operation confirmations match independently accepted behavior. The Go executor does not use the Rust actuator.
5. Stage the complete NixOS source generation without activation, verify every credential path and release-state lease, and capture rollback instructions that retain the latest durable security state.
6. Execute the independently authorized switch with explicit acknowledgement that in-memory owner sessions will terminate. Verify both new services' exclusive state leases, Cloudflare-signed identity, failure-denial paths, audit receipts and protected health checks. Do not use NanoKVM root to bypass the release authority.
7. Run separately authorized rollback verification. Keep the previous implementation available only for recovery until state compatibility and production behavior are certified.

Production Compute workloads, Projects database, artifact volumes and Kubernetes hardware reservations must remain unchanged throughout.

## Browser qualification

The web console is built from `ui/web` after an immutable npm dependency lock is committed. Source components use the upstream shadcn/ui MIT license; provenance is recorded in `ui/web/THIRD_PARTY_NOTICES.md`.

The browser performs no privileged process execution. It calls only same-origin Go routes and `/ui/session`; identity and CSRF enforcement remain server-side. A page that has no working service must show an explicit error, not fabricated data.

`scripts/verify-go-typescript-platform.sh --backend` validates Go tests and NixOS selectors. `--full` additionally requires the committed npm lockfile and runs `npm ci`, strict TypeScript typechecking and the production Vite build.

A browser build is only one acceptance gate. Before retiring `ui/console`, compare every deployed view with the React counterpart: task and recipe writes, personal planning, monitoring, project/artifact handling, Engineering submission and recovery, device controls, session behavior, accessibility, browser CSP, logout and deep links. The current React shell implements authenticated navigation, monitored resource views and bounded verification-run submission; not every existing Dioxus workflow has been migrated.

Do not swap the production console package until the static bundle is independently packaged, a protected release has been approved, API parity is demonstrated, and immediate rollback is documented.

## Remaining native components

The existing repository still contains Rust implementations of motivation, native accelerator integration, sandboxing and other hardware-specific modules. Selecting Go security and executor does **not** remove those dependencies or certify equivalent Go replacements. Such components require separate functional equivalence tests against approved device simulators and hardware boundaries before retiring their Rust source.

Go should own the standard host-facing interfaces; specialized vendor tools and VHDL remain valid where required to compile FPGA or accelerator programs.

## Acceptance criterion

The migration is complete only when the Go authority and Go executor are independently certified and deployed, the TypeScript/shadcn console is the verified production interface, retained records and operator custody are preserved, and ordinary researchers can submit projects and retrieve build artifacts without requiring new infrastructure privileges. Source compilation, draft pull requests and mock tests alone do not establish production completion.
