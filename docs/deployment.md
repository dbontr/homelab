# Deployment

## Source of authority

Git is authoritative for reviewed source and declarative desired state. Protected runtime configuration, credentials, media, application databases, and artifacts are not part of the source repository.

The active fleet is not automatically changed by commits to this repository. Deployments are performed through a separate development and release mechanism. NanoKVM is an administration channel, not a runtime dependency or secret-management system.

## Validation gates

1. Build and test each Go module with the toolchain required by its go.mod.
2. Evaluate all five NixOS host configurations from the root flake.
3. Verify API and MCP discovery, authentication, tool execution, worker isolation, and error handling.
4. Confirm media mount points and network routing invariants before activation.
5. Apply one independently replaceable service at a time, verify health, and retain the prior executable or NixOS generation for rollback.
6. Cut over primary MCP ingress only after the consolidated endpoint passes live acceptance.

The platform source currently preserves existing service deployment definitions as imported code. Runtime activation of consolidated services and decommissioning of originals are separate, testable operations.

## Recovery

Source rollbacks use Git revisions and NixOS generations. Media and other uniquely valuable runtime data must not be deleted as part of the transition. Automatic state backups are not enabled by this repository.
