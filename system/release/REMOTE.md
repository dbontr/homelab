# Authenticated fleet release — source-level design

The deployment architecture separates three independent authorities:

1. The owner signs into the protected browser portal on the security host with password and TOTP. Its session remains revocable, with a 120-minute idle limit and a 12-hour absolute limit.
2. Each host executor has an operator-provisioned TLS server identity. Only a coordinator with a trusted client certificate and matching DNS SAN may call the typed executor API. The original signed owner assertion travels in an HTTP header, not an MCP tool argument.
3. Each remote executor asks the central `homelab-securityd` for **every** authorization decision using a separate mutually authenticated TLS connection. The central service verifies the signed owner identity, current session, operation, target and exact canonical arguments. Its remote listener exposes only `POST /v1/authorize`; it never exposes login, confirmation, revocation or vault endpoints to workers.

The NixOS modules leave both TCP endpoints **disabled by default**. The separate staging endpoint requires the exact `infrastructure__release_stage` policy and returns a host-bound manifest without activating it. Operators must explicitly provide private-IP listeners, TLS certificates, private keys, trust anchors, certificate DNS identities, permitted release operations and host-specific security policies using protected systemd credentials. No TLS secrets belong in Git or in MCP arguments. The executor still denies activation when `allowReleaseActivation = false`, even if a client presents a valid certificate.

## Fleet behavior

`release.RemoteReleaseAgent` implements the existing `HostReleaseAgent` interface and observes the local release journal. `release.DurableFleet` journals a random transaction identity, ordered host targets, and each intent **before** sending a remote command. It writes owner-only records atomically and synchronizes the containing directory. Rollouts proceed compute-first and control-last, and successful hosts are rolled back in reverse order after failure.

The trusted `FleetOrchestrator` writes a private, immutable plan record after all host manifests have been validated. Activation accepts only the opaque plan identity and acquires the same fleet journal lock before loading the plan and writing activation intent. An owner or gateway cannot substitute arbitrary per-host release IDs at activation time. A fully resolved transaction and its plan can be cleared together; ambiguous outcomes cannot be cleared. 

A lost reply is **not proof of either success or failure**. Unknown host outcomes require signed, authorized journal observations with an exact matching release ID; the coordinator never blindly retries an ambiguous switch. Unresolved transactions remain in an attention-required state, and only completed or fully reverted transactions can be cleared.

## Deliberate remaining limitations

These are building blocks, not an automatically deployable five-host system. Remote staging from a clean, trusted checkout with an exact Git commit is available through the protected host executor. Fleet stage plans validate every manifest against the host and revision. Each host must already have the matching Git source and offline Nix inputs; source provisioning and Nix store closure distribution are not implemented. Host credential provisioning, a trusted fleet API/CLI that constructs agent maps, fleet-wide authorization preflight and owner confirmations, health checks beyond existing local service probes, router reachability watchdogs, and live multi-node verification remain outstanding. Security policies must include exact `infrastructure__release_status`, `infrastructure__release_activate` and `infrastructure__release_revert` target entries. No host is activated by importing this package or enabling the NixOS module source in a repository.

Tests exercise fake agents, transaction persistence, recovery, and in-process TLS clients. They do not establish successful NixOS switches or safe router failover. See root `REFERENCES.md` for standards.
