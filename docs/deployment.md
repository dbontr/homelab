# Deployment

## Authority and boundaries

Git is authoritative for source and declarative desired state. Protected credentials, runtime records, media and application databases remain outside Git.

The running fleet is not changed by repository commits. A separate development and release pipeline, administered through NanoKVM for the present configuration, validates and activates accepted revisions. The main MCP does not inherit the release controller's credentials.

## Platform service

The NixOS platform module is located at nixosModules/platform.nix and is disabled unless explicitly enabled. It packages the standard-library Go application, binds to loopback and reads credentials from systemd-protected files. Configure services.homelabPlatform.tokenFile with a token containing at least 32 characters. The optional calendarURLFile contains the read-only Proton Calendar sharing link and must not be committed.

The service's private state is stored under /var/lib/homelab-platform. A protected local approval socket resides at /run/homelab-platform/approval.sock. A trusted Saturn administrator can review pending challenges and approve an exact operation using the homelab-approve command. The socket must never be proxied through the public ingress. Configure the optional Seerr URL and protected API key file together; private HTTP is accepted only for explicitly configured private or loopback IPs. Optional backend MCP URLs and token files are configured independently for Workspace, Learning and Health. Protected network ingress must authenticate remote clients before routing traffic to the platform.

## Validation gates

1. Build and test the Go platform and each imported Go module using its declared toolchain.
2. Evaluate every host configuration from the root Nix flake.
3. Verify API and MCP discovery, authorized/unauthorized requests, backend failure behavior and calendar access.
4. Verify that media mount points, networking, and VPN confinement are unaffected.
5. Validate application state behavior, independent worker failure and service rollback.
6. Activate one replaceable component at a time and verify the live result before routing production traffic.
7. Retire source repositories and rename the successor only after operational acceptance.

The imported upstream service modules retain existing runtime release references. Replacing those with locally built, verified packages is part of runtime integration, not implied by code placement.

## Recovery

Revisions and NixOS generations permit rollback of code and configuration. Existing media and uniquely valuable data must not be deleted during integration. This repository does not enable automatic state backup scheduling.
