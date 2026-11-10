# Deployment

## Authority and boundaries

Git is authoritative for source and declarative desired state. Protected credentials, runtime records, media and application databases remain outside Git.

The running fleet is not changed by repository commits. Saturn does not run a repository self-push timer; source delivery is handled by the independent release process. A separate development and release pipeline, administered through NanoKVM for the present configuration, validates and activates accepted revisions. The main MCP does not inherit the release controller's credentials.

## Platform service

The NixOS module is located at nixosModules/platform.nix and is disabled unless explicitly enabled. It packages the standard-library Go application, binds to loopback and reads credentials from systemd-protected files. Configure services.homelabPlatform.tokenFile with a token containing at least 32 characters. The calendar feed is preferably managed by the independent `services.homelabCalendar` service. Set `enable = true`, `feedURLFile` to the protected HTTPS sharing-link file, and `tokenFile` to a distinct protected service credential file. The platform automatically connects to the private calendar listener when both services are enabled, without loading the sharing-link credential. Do not enable `services.homelabPlatform.calendarURLFile` concurrently; that option remains available only for deployments without the separate calendar service. Never commit the sharing URL or either secret.

Device policy comes from the device entries in hardware.nix; enrolled node entries determine the NixOS hosts. SSH-mode equipment is never a deployment target. Nix supplies it to the Go application as JSON without device credentials. The service's private state is stored under /var/lib/homelab-platform. A protected local approval socket resides at /run/homelab-platform/approval.sock. A trusted Saturn administrator can review pending challenges and approve an exact operation using the homelab-approve command. The socket must never be proxied through the public ingress. Configure the optional Seerr URL and protected API key file together; private HTTP is accepted only for explicitly configured private or loopback IPs. Optional backend MCP URLs and token files are configured independently for Workspace, Learning and Health. Protected network ingress must authenticate remote clients before routing traffic to the platform.

## Validation gates

1. Develop and validate without activating a NixOS generation or modifying running services. Build and test the Go platform and each imported Go module using its declared toolchain.
2. Evaluate every enrolled host configuration from the root Nix flake.
3. Verify API and MCP discovery, authorized/unauthorized requests, backend failure behavior and calendar access.
4. Verify that media mount points, networking, and VPN confinement are unaffected.
5. Validate application state behavior, independent worker failure and service rollback.
6. Activate one replaceable component at a time and verify the live result before routing production traffic.
7. Retire source repositories and rename the successor only after operational acceptance.

Use `bash scripts/stage-release.sh /absolute/release/directory` on an authorized self-hosted builder to stage the Go platform, Workspace controller and worker, Learning, Health, approval utility and Dioxus frontend together. It writes a SHA-256 manifest and does not start or replace any service. The imported Workspace Kubernetes builder supports an explicitly configured OCI registry, digest-pinned rootless BuildKit image and separately scoped registry credentials; no GitHub Actions minutes are needed for local source builds. The Nix deployment definitions still refer to existing runtime releases until an operator explicitly selects verified local artifacts. Source integration does not imply live cutover.

Monitoring is configured through `services.homelabPlatform.monitoring` with loopback Prometheus/Loki sources and source-managed exporter targets for Saturn, Jupiter, Venus, Mercury and Janus. Existing Grafana and monitoring containers remain available. The optional Open Library catalog requires `services.homelabPlatform.books.contactFile` with the contact email supplied through a protected systemd credential. The Dioxus frontend has a separate opt-in console NixOS module; the original homepage remains active unless an operator enables `replaceHomepage` after acceptance. See [observability](observability.md), [books](books.md) and [self-hosted releases](self-hosted-release.md).

## Recovery

Revisions and NixOS generations permit rollback of code and configuration. Existing media and uniquely valuable data must not be deleted during integration. This repository does not enable automatic state backup scheduling.
