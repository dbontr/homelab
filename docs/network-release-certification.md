# Networking and protected NixOS release qualification

**State:** source-only proposal, no production cutover authorization. All steps affecting deployed ingress, DNS, firewall, SSH, tunnels, NixOS generations, Kubernetes, Compute or Projects are prohibited in this branch.

## Immutable source and authority

The staging input is a full, lowercase 40-hex Git commit, not a branch, tag, worktree name or user path. Every host must possess the exact commit in a clean, operator-owned checkout with offline Nix inputs. Both the Rust independent executor and Go fallback construct `git+file://…?rev=<40-hex>#nixosConfigurations.<host>.config.system.build.toplevel`. A staged manifest records a random, opaque 48-hex release ID, host, revision, Nix store target and timestamp. The manifest resides in the root-controlled private release state.

Release authorization is **never delegated to the MCP gateway**. The optional fleet coordinator owns protected mTLS client credentials and exposes only a private Unix socket. An owner session and operation-specific step-up are independently verified by Security; the host executor separately checks signed authorization and its exact host/release arguments. The MCP gateway cannot provide certificate keys, NixOS commands, arbitrary release directories or an unscoped execution identity. Release tools appear only when `services.homelabPlatform.fleetSocket` is explicitly configured to the protected coordinator socket. The fleet coordinator, private executor mTLS listener and central host authorization are disabled by default.

Stage, prepare/inspect, activate, status, recover and clear are distinct operations. The coordinator persists a private, checked fleet plan; activation accepts its opaque ID, never replacement targets. Activation writes a transaction and per-host intent *before* sending operations. Host execution request identities are derived deterministically from transaction, host, release and operation; a lost response does not grant permission to create another request identity. A started or indeterminate attempt must be reconciled against the authorized host journal. An unresolved release remains blocked and cannot be cleared. Rollback is reverse-order; Saturn is **last** in any compute-first rollout.

The Rust executor's protected status now verifies terminal journal state against the actual NixOS system profile and the immutable staged manifest; receipts include source revision, host, target and previous system paths. If a prior-generation rollback already completed before a crash, recovery checks health and records the outcome without switching the same generation again. A previously attempted staged ID cannot be activated a second time.

## Fixed five-host topology

| Host | Source role | IP / interface expectation |
| --- | --- | --- |
| Saturn | router, gateway, K3s control plane | VLAN10 management `10.77.10.1/24`; VLAN20 services `10.77.20.1/24`; VLAN30 compute `10.77.30.1/24` |
| Jupiter | compute worker | `10.77.30.229/24`, `enp129s0`; gateway/DNS `10.77.30.1` |
| Venus | compute worker | `10.77.30.230/24`, `enp2s0`; gateway/DNS `10.77.30.1` |
| Mercury | compute worker | `10.77.30.231/24`, `enp1s0`; gateway/DNS `10.77.30.1` |
| Janus | compute worker | `10.77.30.232/24`, `enp0s31f6`; gateway `10.77.30.1` |

The canonical inventory is `hardware.nix`. Source inspection establishes the expected addresses, not that they are reachable on the deployed network. Saturn's `router.nix` requires explicit physical switch isolation, separates WAN and trunk, and allows management SSH (TCP 22) and K3s (TCP 6443) on the management VLAN; compute-network ingress to K3s is defined in `router-rollout.nix`. Confirm worker SSH management and source enrollment individually before approving any later fleet release. Existing production Compute and Projects are outside this workstream and must not be restarted, replaced or drained for qualification.

## Bounded acceptance gates

NixOS declares the following read-only critical service checks on Saturn: `systemd-networkd`, `systemd-resolved`, `dnsmasq`, `sshd`, `cloudflared-saturn`, `k3s`, `homelab-platform`, `homelab-mcp`, `homelab-api`, `homelab-securityd`. These health-only names do **not** expand the executor's service-mutation allowlist.

The independent executor additionally checks fixed private TCP connections: management SSH `10.77.10.1:22`, services DNS `10.77.20.1:53`, compute DNS `10.77.30.1:53`, protected ingress listener `127.0.0.1:8104`, platform `127.0.0.1:8300` and MCP interface `127.0.0.1:8401`. Each service inspection has an individual 15-second bound and each private socket attempt has a three-second bound. Public, wildcard, hostname and duplicate socket policies are rejected. Any failed check prevents a successfully reported activation; recovery also checks declared health.

A successful TCP handshake is **not** proof of application authentication, DNS resolution, Cloudflare route correctness, external reachability or complete Kubernetes resilience. Those are separate acceptance checks and may only be run read-only in an independently approved cutover window.

## Monitoring Access acceptance

The source ingress for `monitor.dbontr.me` is conditional on a non-null 64-character lowercase hexadecimal Access audience. It routes through the existing Cloudflare Tunnel to the local Grafana HTTP origin at `127.0.0.1:3000` and requires signed Access validation for the `dbontr` team. PR #195 established the pinned audience. Changes here add an explicit Nix assertion; they do not create the external DNS record, Access application or its owner policy.

After independent approval **and without changing routing**, an authorized operator may run:

```bash
bash scripts/accept-monitor-ingress.sh /absolute/path/to/owner-only-cloudflare-cookie-file
```

The test uses short, nonredirecting HTTPS requests against `https://monitor.dbontr.me/api/health`: anonymous requests must return a denial/Access redirect, while a valid owner-authenticated session must return HTTP 200 with Grafana reporting a healthy database. The cookie file must belong to the caller, be nonempty and have no group/world permissions. Session values are neither logged nor accepted as command-line arguments. Do not treat fixture tests as live Access certification.

## Explicitly withheld production prerequisites

- Security workstream review of shared Rust `policy.rs`, `release.rs` and `executor.rs`; test exact operation/target/owner step-up policies and audit reconciliation.
- Independent provision of root-owned TLS credentials, host identities, coordinator trust roots and fixed private listener addresses, without storing secrets in Git or the gateway.
- Exact commit and complete offline Nix closure on each host, authenticated host-to-host stage/preview and read-only management network tests.
- Confirmation that Cloudflare DNS, tunnel routing, owner-only Access application/audience and Grafana origin match the source. Run the monitoring acceptance above only when permitted.
- Offline `nix flake check --offline --no-build`, all five NixOS `system.build.toplevel.drvPath` evaluations, `cargo test --locked --offline` in `services/executor`, focused Go release/transport tests, and `bash scripts/verify-offline.sh --all` from an isolated clean checkout with a pinned toolchain.
- Separately authorized production cutover plan, recovery operator, pre-cutover profile snapshot and explicit maintenance window. Verify live SSH, DNS queries, MCP authenticated initialization, tunnel connectivity and monitoring access before and after any later activation. Do not use NanoKVM to bypass the executor.
- Until transaction-scoped multi-host approvals can survive the independent confirmation expiry, do **not** rely on unattended five-node or router cutover. An expired approval and ambiguous result are hard stops requiring owner reauthorization or reconciliation, not blind retries.

**Current validation boundary:** Offline fixtures can verify the monitoring acceptance script. An environment without Nix, Rust or the pinned Go toolchain cannot establish offline Nix evaluations, Rust compilation or full cross-language regressions; such checks must remain unverified in the draft PR rather than being marked passing. No production network, security service, Kubernetes resource or NixOS generation should be changed by this source-only work.
