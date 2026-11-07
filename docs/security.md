# Security and credentials

## Trust boundaries

The primary MCP and HTTP API operate as a single-user application interface. Application operations use credentials specific to their target service. The presence of a tool does not imply authority to execute it. The target service must enforce its own authorization.

Cluster changes and shared software-library administration require an independently enforced approval step. The platform creates a short-lived challenge bound to the exact tool and canonical arguments. A local administrator reviews the operation on Saturn and approves it over the Unix socket. The grant is single-use and expires after two minutes; unapproved challenges expire after five minutes. The user repeats the identical MCP tool call after approval. Shared software-pool registration and review are privileged operations.

The approval socket is created under a private runtime directory with mode 0600. Only a trusted operating-system administrator should have permission to connect. Neither the socket nor its handler may be exposed through the public HTTP or MCP ingress. The administrative CLI is intended for a trusted login through NanoKVM or another securely authenticated local management channel.

This approval mechanism prevents ordinary MCP sessions from executing elevated tools without local intervention. It is **not** a security boundary against arbitrary code execution inside the gateway process itself. A future cluster executor must hold its own credentials and independently validate privilege grants. Until that executor is available, a general-purpose cluster mutation backend must not be enabled with an unrestricted administrator credential.

## Authentication and software accounts

Do not send passwords, recovery codes, API keys, product license credentials, or vendor login cookies through ChatGPT messages, tool parameters, project artifacts, shell history, or Git. Enter them directly into the relevant provider's trusted login page, or into an authorized local credential-entry interface when one is implemented.

Provider OAuth authorization-code and device-authorization flows can connect accounts without exposing the user's password to the assistant. Where a provider supports only ordinary username/password authentication, the credentials remain under protected local storage and only approved operations may use them. Future development of a credential broker must separate the credential owner's authentication from the AI runtime.

Existing Cloudflare Access protects external ingress. It is not exposed as a general-purpose Cloudflare management tool in the primary MCP. Native Seerr and calendar credentials are read from systemd LoadCredential files. Private ICS sharing links are credentials and must never be committed to Git.

## Operational separation

The primary assistant can request authorized application operations. The development and deployment pipeline is a separate principal and never inherits the primary MCP's token. Changes to network routing, firewall, machine identity, K3s admission, or host software need operator confirmation and independent service-level authorization.

Media data and all runtime credentials remain outside Git. The absence of automatic backups is an explicit operational risk; repository reproducibility cannot reconstruct missing personal data.

## SSH-mode equipment

The hardware inventory is declarative and is not proof of connectivity. The KV260 inventory currently permits SSH connection and inspection only, with compute allocation disabled. These are entry-specific policy values, not restrictions associated with hardware models or owners. SSH equipment can be added or removed without changing Go code; it is never enrolled as a NixOS host or compute worker. An ordinary non-root SSH login must enforce host-key verification and explicitly permitted operations. No worker or controller is installed remotely through this mode. The catalog has no SSH login target until the owner configures one. Credentials and trusted host keys remain outside Git. The SSH account does not imply permission to install software, elevate privileges, reprogram the FPGA, change firmware, or use USB/JTAG. The actual device executor must enforce authorization and bind the real device lease to the configured device identity; preflight catalog checks are not sufficient by themselves.

A managed GPU, NPU, FPGA or other accelerator may use Rust for narrowly scoped device work when necessary. Rust does not confer any additional operating-system permissions.


## Independent execution and provider credentials

The `system/security` package defines short-lived HMAC grants bound to a target, action and canonical operation arguments. An independent executor records consumed grants to prevent replay after restart. The MCP gateway must neither issue these grants nor own the executor's key. The executable handler registry rejects operations the owner has not explicitly registered.

The OAuth broker implements PKCE for configured HTTPS providers and a loopback callback. Authorization codes, verifier values and resulting access tokens must remain in the trusted local credential process. The broker library can exchange an OAuth authorization code with a configured HTTPS token endpoint and encrypt the token response in an AES-256-GCM vault, returning only a credential handle. An operator-only Unix-socket approval handler verifies a locally stored PBKDF2-HMAC-SHA256 passphrase record and issues short-lived operation grants for explicitly allowlisted operations. The verifier applies a bounded lockout and the socket requires a private directory and owner-only file mode. This code has no production daemon, user-facing unlock prompt, live credential provider, or protected gateway-to-executor IPC. Independent executor authorization remains a release requirement. The repository test suite exercises mock operations without live credentials.

## External request boundaries

The API rejects browser requests with an unapproved `Origin` and requires explicit HTTPS origins for remote clients. Non-browser clients without an `Origin` header continue to use bearer authentication. The NixOS option `services.homelabPlatform.allowedOrigins` configures exact permitted origins; wildcard origins are not supported.

Federated MCP calls are restricted to operations advertised by their backend. Upstream replies have a bounded size and must match the request's JSON-RPC ID. Sensitive Workspace operations require local administrator approval, while unrecognized Workspace operations fail closed into an approval-required state. Upstream error details and private calendar-sharing URLs are not returned to API callers.

Tool arguments use canonical JSON for both approval and execution. Duplicate keys, excessive nesting and numeric rounding ambiguities are rejected. An approved operation cannot acquire different arguments through a second JSON interpretation.

The local administrator challenge is a gateway-level policy check. It is not a replacement for independently enforced authorization inside compute, software-installation, GitHub or cluster executors.
