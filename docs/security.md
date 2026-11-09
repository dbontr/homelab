# Security and credentials

## Trust boundaries

The primary MCP and HTTP API operate as a single-user application interface. Application operations use credentials specific to their target service. The presence of a tool does not imply authority to execute it. The target service must enforce its own authorization.

The independent `homelab-securityd` service can hold an owner administrative session authenticated with a password and a six-digit TOTP. Its optional NixOS module is disabled by default. When configured, the primary gateway checks restricted operations through a group-limited Unix client socket. It has no access to the password verifier, TOTP secret or owner-only authentication socket.

Authenticated sessions have a 120-minute inactivity limit and an absolute 12-hour lifetime. Approved routine operations may reuse the active session. The owner or authorized MCP client can revoke it immediately. Destructive operations require an independent, owner-only confirmation of the exact action, resource and canonical argument digest with a fresh password and TOTP. Confirmations expire quickly and can be consumed only once. No administrative token is returned to the model. See [privileged sessions](privileged-sessions.md) for local enrollment, operation policy and commands.

The legacy gateway-local approval socket remains available for development installations without the independent security client. It is not a security boundary against arbitrary code execution in the gateway process and must not authorize production cluster administrator credentials. Even with the independent session service, any downstream executor that accepts a broadly privileged credential from the gateway could bypass the authorization check. Such executors must independently validate grants and resource permissions before production write access is enabled.

## Authentication and software accounts

Do not send passwords, recovery codes, API keys, product license credentials, or vendor login cookies through ChatGPT messages, tool parameters, project artifacts, shell history, or Git. Enter them directly into the relevant provider's trusted login page, or into an authorized local credential-entry interface when one is implemented.

Provider OAuth authorization-code and device-authorization flows can connect accounts without exposing the user's password to the assistant. Where a provider supports only ordinary username/password authentication, the credentials remain under protected local storage and only approved operations may use them. Future development of a credential broker must separate the credential owner's authentication from the AI runtime.

Existing Cloudflare Access protects external ingress. It is not exposed as a general-purpose Cloudflare management tool in the primary MCP. Native Seerr and calendar credentials are read from systemd LoadCredential files. Private ICS sharing links are credentials and must never be committed to Git.

## Privileged execution evidence

The independent executor appends a private, fsynced JSON-lines record before every approved side effect and again after the handler reports its outcome. Entries include an operation ID, canonical argument digest, exact target, operation name, time, and execution state; raw arguments and credentials are never recorded. If the journal cannot record a start event, execution is denied. If a process fails after a started event, the unresolved record means the outcome is **unknown**, not necessarily failed or rolled back. The executor keeps journal custody outside the assistant process, and the release/operator must retain and rotate this file under its own security policy.

## Operational separation

The primary assistant can request authorized application operations. The development and deployment pipeline is a separate principal and never inherits the primary MCP's token. Changes to network routing, firewall, machine identity, K3s admission, or host software need operator confirmation and independent service-level authorization.

Media data and all runtime credentials remain outside Git. The absence of automatic backups is an explicit operational risk; repository reproducibility cannot reconstruct missing personal data.

## SSH-mode equipment

The hardware inventory is declarative and is not proof of connectivity. The KV260 inventory currently permits SSH connection and inspection only, with compute allocation disabled. These are entry-specific policy values, not restrictions associated with hardware models or owners. SSH equipment can be added or removed without changing Go code; it is never enrolled as a NixOS host or compute worker. An ordinary non-root SSH login must enforce host-key verification and explicitly permitted operations. No worker or controller is installed remotely through this mode. The catalog has no SSH login target until the owner configures one. Credentials and trusted host keys remain outside Git. The SSH account does not imply permission to install software, elevate privileges, reprogram the FPGA, change firmware, or use USB/JTAG. The actual device executor must enforce authorization and bind the real device lease to the configured device identity; preflight catalog checks are not sufficient by themselves.

A managed GPU, NPU, FPGA or other accelerator may use Rust for narrowly scoped device work when necessary. Rust does not confer any additional operating-system permissions.


## Independent execution and provider credentials

The `system/security` package provides replay-protected operation-bound grants, a strict canonical JSON parser, an explicit handler registry, OAuth credential-exchange utilities, and the independent password/TOTP administrative session service. The `homelab-securityd` process owns its verifier, authenticator key, temporary session state, and destructive confirmations. It does not bind a network port. A trusted local operator uses `homelab-admin` through an owner-only Unix socket; the gateway has only a separate client socket for permission checks, session status and revocation.

The session service enforces finite configured operation/resource policies and fails closed on absent credentials, expired sessions, malformed state, TOTP replay, and unavailable sockets. Failed credential attempts and accepted TOTP counters are persisted in an owner-only state file; active sessions are not persisted across restarts. The security service itself never executes arbitrary shell commands or grants broad cluster administrator rights.

The platform's gateway-to-security authorization integration is a policy check, not yet a substitute for the cluster executor's own enforcement. Privileged backend tokens, trusted hardware identities, and operation handlers must be isolated from the gateway at their real enforcement boundaries. Deploying the session service alone does not complete the privileged cluster executor or permit a production security sign-off.

## Owner browser sign-in

A configured MCP tool returns only the exact owner-controlled HTTPS authentication URL. ChatGPT opens it as a browser destination; it does not collect password or TOTP through chat, an MCP input, or a model-visible widget result. The optional portal terminates its private traffic on a separate Unix socket held by `homelab-securityd`. It requires a dedicated owner-only Cloudflare Access policy, signed tunnel-origin assertion verification, and an exact Host/Origin plus single-use CSRF check. Successful authentication activates the 120-minute idle, 12-hour maximum session on the independent service. Confirmations remain exact-operation bound and require fresh local credentials.

The portal is not a general cluster administration API, and activating it does not give the primary MCP or the web proxy any executor credentials. Deployment must verify that the public hostname and its Access audience match before admitting traffic.

## Credential access and security audit

Encrypted vendor credentials reside in the independent security process, not in the MCP gateway or a project workspace. The owner-only Unix socket exposes credential inventory and enrollment/revocation operations. Each enrollment or deletion requires the password and a fresh TOTP. Only opaque handles are returned; a bounded single-use capability authorizes exact operation, resource, and canonical arguments before a trusted executor callback may use plaintext. Do not expose a generic secret-retrieval route or place the encryption key in Git.

Security event records are collected from the existing systemd journal into Loki. The read-only `monitoring_security` tool and `/v1/monitoring/security` native endpoint return bounded event counts for authentication denial, session changes, credential changes and privileged execution. Missing logs or unavailable collectors never count as evidence of no incidents. No raw secret-bearing journal lines are included in the summary.

## External request boundaries

The API rejects browser requests with an unapproved `Origin` and requires explicit HTTPS origins for remote clients. Non-browser clients without an `Origin` header continue to use bearer authentication. The NixOS option `services.homelabPlatform.allowedOrigins` configures exact permitted origins; wildcard origins are not supported.

Federated MCP calls are restricted to operations advertised by their backend. Upstream replies have a bounded size and must match the request's JSON-RPC ID. Sensitive Workspace operations require local administrator approval, while unrecognized Workspace operations fail closed into an approval-required state. Upstream error details and private calendar-sharing URLs are not returned to API callers.

Tool arguments use canonical JSON for both approval and execution. Duplicate keys, excessive nesting and numeric rounding ambiguities are rejected. An approved operation cannot acquire different arguments through a second JSON interpretation.

The local administrator challenge is a gateway-level policy check. It is not a replacement for independently enforced authorization inside compute, software-installation, GitHub or cluster executors.
