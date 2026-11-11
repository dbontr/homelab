# Privileged owner sessions

Security and executor daemons use separate process-lifetime state ownership leases. The trusted local release CLI acquires the executor's release-state lease before stage, activate, revert or recover operations; it refuses writes while the executor owns the state. Read-only status inspection remains available, and manual recovery requires stopping the executor from a trusted out-of-band terminal. Each daemon must obtain the lock in its protected state directory before reading durable state or accepting privileged requests. No backup, recovery, or replacement daemon may write these directories while an owner holds the lease.

The independent `homelab-securityd` process owns password verification, the TOTP secret, session state, and confirmation decisions. Its password verifier, TOTP secret, vault encryption key, and verified owner-identity file must be owner-only regular files; group or other read access is rejected on the opened file descriptor. Public signing-key and policy material may remain read-only and accessible without secret-file permissions. Its default configuration binds no TCP listener. The optional remote authorization listener uses mutual TLS and exposes only authorization decisions to allowlisted executor certificate identities. Security-daemon and executor TLS identities load private keys only from owner-only regular files; final-path symbolic links and oversized PEM material are rejected. Public certificate chains and pinned CA roots are read under bounded, read-only configuration rules. Executor clients reject authorization replies with more than 8 KiB of JSON or 16 KiB of response headers, even if an otherwise valid decision appears at the beginning. TLS and response-header waits have independent time limits; failed or malformed replies never grant privileges. Its owner socket is mode 0600. Its gateway socket is mode 0660 with membership restricted to `homelab-security-client`. A separate systemd service account holds the credential files, and the platform gateway must not receive them.

## Authority state compatibility

The Rust authority kernel uses the same owner-only `.securityd.lock`, `authenticator.json` schema, and AES-256-GCM `credentials.json` record format as the Go security service. Ownership is exclusive; the two implementations must never run with write access to the same directory concurrently. Session and confirmation state is never persisted across process restarts. The security unit provides a mutually exclusive Go or Rust implementation selector. It retains the same state directory, Unix socket paths and service identity, so the implementations cannot run simultaneously under the managed unit. A rollback stops the active service, confirms that it has released its state lease, and starts the other implementation only after validating replay and vault state.

## Rust authority admission

The Rust authority kernel must validate a private copy of the authentication state and encrypted vault before it can become an operational service. Its offline security-preflight binary inspects an independent state copy without acquiring any writer lock or creating files, reads the stored replay and vault records, verifies each AES-GCM authentication tag, checks the finite operation policy, and validates any configured signing keys and service credential grants. It refuses the default live state path and requires an independent owner-only copy. Run it through Cargo with the same security configuration variables and --state-dir pointing at the copied directory. Do not run two state owners against one directory.

`services.homelabSecurity.implementation` defaults to `"go"`. The alternative `"rust"` selects the Rust binary inside the same `homelab-securityd.service`; its Nix package requires a committed, resolved `Cargo.lock` and enforces the crate test suite during its build. Do not select it until Go/Rust compatibility, full Rust compilation, adversarial authorization, and isolated rollback acceptance have passed. A source-level kernel and a successful state preflight alone do not establish deployed security correctness. The Go daemon remains active while those checks are incomplete.

## Scoped service credentials

The independent owner authority binds each vault grant to a single verified
TLS client DNS identity, credential alias and handle, independently approved
operation, and exact resource target. The operator can additionally restrict
a grant to one SHA-256 digest of canonical JSON arguments. Every credential
ticket is bound to the complete actual canonical arguments, even when the
operator does not configure an argument digest.

The authority snapshots the encrypted record revision at configuration time.
The trusted executor must first consume a signed, current, owner-bound
operation authorization. Only the mutually authenticated authorization
response can include a credential ticket. The ticket binds the owner principal
and session ID, TLS client identity, operation, target, argument digest,
credential alias and revision. Tickets expire after at most 60 seconds, are
not persisted across restart, and are invalidated by logout, session expiry
or credential rotation. A ticket can be redeemed exactly once.

The executor redeems a ticket through
`POST /v1/service-credentials/{alias}` on the mTLS listener. Its JSON body
contains `ticket`, `operation`, `target` and `arguments`. The authority
consumes the ticket before comparing its scope and checks the current encrypted
record revision before returning a bounded credential. An alias, certificate,
owner assertion or previously successful operation alone never grants a
credential read. The route is unavailable over the owner, browser and local
gateway sockets. Credential tickets are never returned by the executor's
gateway-facing API.

NixOS declares these restrictions through
`services.homelabSecurity.remote.serviceCredentialGrants` using
`clientDNS`, `alias`, `provider`, `handle`, `operation`, `target`,
optional `argumentsSHA256`, optional `revision`, and optional `expiresAt`.
Only remote identities authorized to request owner operation decisions can
receive tickets in the current executor contract. Credential-only mTLS peers
cannot mint their own approval or obtain secrets from an alias alone.

The Compute controller binds vault-backed GitHub, GitLab and research
embedding credentials to individual authenticated MCP or HTTP operations. A
request-local provider scope obtains an independent owner authorization on
first credential use through the same mTLS identity that will redeem the
one-use ticket. The authorization context contains the exact tool name and
arguments (target `controller`), or the HTTP method, path, query and bounded
JSON body (operation `controller__http_get`, `controller__http_post`,
and the corresponding supported methods; target `controller`). The
independent owner policy must explicitly permit the operation and target,
and its service credential grants must name the Compute controller's
certificate DNS identity as an **authorizing** peer.

The signed owner assertion is supplied by the authenticated ingress and is
verified by the security authority, not inferred from a controller bearer
token or generated by an MCP tool. A unified Workspace backend may receive
this assertion only when the platform's
`integrations.workspace.forwardOwnerAssertion` option is explicitly
enabled for the trusted backend. Other integrations never receive it.
Without a valid assertion, remote operation approval or a matching credential
grant, vault-backed provider operations fail closed; unrelated local tools
remain available. The controller caches a redeemed provider credential only
within the initiating request, never in the persistent server or worker state.
Background jobs and scheduled callbacks cannot borrow that credential or
mint their own owner approval.

The platform's model-planning and Seerr clients use the same one-operation
contract. A model request binds `model__completion` to the exact selected
model and a SHA-256 digest of the encoded prompt. Seerr binds
`media__seerr_request` to the configured service and the actual method,
path, and request-body digest. The independently verified owner identity
must be present on the caller context at the time of use. Every outbound
request receives a newly approved one-use ticket; neither model input nor a
provider token enters MCP tool arguments. An automatic model continuation
cannot claim this authority merely because a preceding interactive step
had it.

The same constraint applies to other applications and service credentials:
an ambient `TokenSource(alias)` without an authenticated operation cannot
read the vault. Fixed service-to-service credentials required for health
checks, federated MCP discovery, or autonomous engineering jobs use separately
restricted service identities and protected file custody, not a fabricated
human authorization ticket. NixOS rejects owner-vault aliases for these
unattended service identities. A future delegated service-capability
mechanism must independently authenticate and bound each unattended job
before such aliases can be supported.

## Restricted Rust executor

The privileged host executor has one implementation selector:
`services.homelabExecutor.implementation = "go" | "rust"`. The default is
`"go"`, and either selection owns the same
`/var/lib/homelab-releases/.executord.lock` for the daemon lifetime.
Only one owner may append to `executor-actions.jsonl` or change the NixOS
release journal. Rust uses the existing audited JSON event fields and durable
`started` → `accepted`/`succeeded`/`failed` transitions. A request
acknowledged by Kubernetes or Workspace is `accepted`, not a completed job.
An unpaired `started` audit record remains unresolved when a process crashes
or an executor loses custody. It is never silently labeled failed or retried.
A separately authorized coordinator may read `GET /v1/audit/status`; a trusted
root console can run `audit-inspect unresolved` after the active executor
releases its state lease.
A NixOS activation/reversion is `completed` only after host-local health
checks.

The Rust daemon owns the same
`/run/homelab-executord/execute.sock` contract as the Go daemon. Requests
must carry a signed owner assertion. The independently authenticated security
service must admit the exact operation, host and canonical arguments before
an audit start entry is written and a command is launched. The gateway cannot
select binaries, shell strings, an arbitrary Workspace tool name, or external
URLs. Service and Kubernetes operations pass through the separately allowlisted
Rust actuator. Workspace operations use fixed MCP calls and a scoped, protected
credential read at the point of use. Credential access never crosses the
gateway boundary.

An optional private TLS listener accepts only one configured coordinator
certificate DNS identity from a trusted client CA. It exposes exact-commit
release staging, staged-generation preflight, independently authorized
operation dispatch and read-only journal observation. Owner-factor login,
credential enrollment and the general vault are never offered by the executor.
Staging accepts a clean exact Git commit and an offline Nix build. Release
activation records the prior NixOS generation before switching; failed health
checks trigger rollback. The host journal preserves interrupted release state
for explicit recovery.

The Rust package is `homelab-executor-authority`. Its code resides under
`services/executor`, and Nix packages the separate `executord`,
`recovery` and copied-state `preflight` binaries. The preflight accepts an
independent owner-only directory, rejects live directory/inode aliases,
replays the protected audit transitions and validates staged manifests and
any release journal before a change of implementation. A reviewed `Cargo.lock` is required before Rust can
be selected in the NixOS module. The root-only
`homelab-executor-recovery.service` is not enabled automatically. It can
be started deliberately after stopping the active executor, and it must
acquire the same lifetime lease before restoring the previous generation.
The Go release CLI remains a separate root-controlled recovery option and
cannot run concurrently with an executor that owns that lease.

The implementation is distinct from release acceptance. The Rust source,
Go/Rust wire compatibility, dependency resolution, state-copy admission,
failure recovery and NixOS expressions must pass their independent checks
before changing the selected service or operating on host credentials.

## Session policy

The owner authenticates with a PBKDF2-HMAC-SHA256 password verifier and a six-digit RFC 6238 authenticator code. TOTP counters cannot be reused, including across security-service restarts; the verifier records accepted counters and failed-attempt lockout in an owner-only state file. Five failures trigger a five-minute lockout.

One authenticated session is active at a time. Its idle lifetime is **120 minutes**, renewed only by an allowed operation or owner confirmation. Its absolute lifetime is **12 hours** from authentication and cannot be extended by activity. Revocation is immediate: the owner or MCP client may close the session, and all pending destructive confirmations become invalid. A service restart also invalidates every session. Authentication and confirmation never return a bearer token to ChatGPT, the MCP, or a compute worker.

A configured operation policy explicitly names permitted actions and resource identities. Any unlisted operation or target is denied. Destructive operations additionally require the owner to inspect an exact action/target/arguments digest and submit the password and a fresh authenticator code through the owner-only socket. The resulting confirmation is valid for at most two minutes, is consumed once, and cannot authorize modified arguments. Denied requests do not refresh session activity.

## Provisioning

Build the `homelab-security` package from the unified repository on a trusted NixOS machine. On Saturn, create a root-owned, mode-0700 credential directory outside Git and the Nix store. From a local trusted terminal, run:

```
sudo homelab-admin enroll --owner OWNER --password-file /srv/homelab/secrets/privilege-password.json --totp-file /srv/homelab/secrets/privilege-totp
```

The command uses `systemd-ask-password` to collect the password without echoing it or putting it in shell history. It writes the password verifier and TOTP secret to distinct mode-0600 files and displays the authenticator enrollment URI **only on the local terminal**. Do not paste that URI, passwords, OTP codes, or recovery data into ChatGPT or GitHub.

Set `services.homelabSecurity.enable = true`, `owner`, `passwordVerifierFile`, `totpSecretFile`, and a finite `policies` list. Configure `services.homelabPlatform.securityClientSocket = "/run/homelab-securityd/client.sock"` only once the new security service and its policy have passed host acceptance. The platform gateway joins the narrow security-client group; it cannot connect to the owner socket. Both NixOS modules are opt-in and keep running infrastructure unchanged until activated.

Use these local operator commands (not MCP tool arguments):

```
sudo homelab-admin login
sudo homelab-admin status
sudo homelab-admin pending
sudo homelab-admin confirm CONFIRMATION_ID
sudo homelab-admin close
```

`pending` returns redacted argument previews and full SHA-256 digests. The owner must inspect the action and target before confirmation. The initial TOTP counter cannot be reused for a destructive step-up; wait for the authenticator's next 30-second code. All owner requests use the owner-only Unix socket. No browser or public ingress route is configured by the security daemon.

## Privileged Workspace delegation

The optional `services.homelabExecutor.workspace` configuration uses a fixed HTTPS Workspace MCP endpoint (or loopback HTTP for a colocated controller) and a protected controller credential loaded only into the privileged executor through systemd. The platform gateway does not receive the controller root token. The executor verifies the credential file's mode and type on the open descriptor for each authorized call and does not follow symbolic links. Vault-supplied tokens have the same length limit as protected file credentials.

The executor accepts four typed operations: stop an allowlisted project workspace, queue an environment build in an allowlisted project, register a software package collection with exact approved image/software/package identifiers, and revoke an allowlisted compute worker. Each call independently verifies the signed owner principal, current session, operation, resource and canonical arguments before the executor reads the credential. All four operations require one-time owner confirmation. The executor forwards only the fixed Workspace MCP tool name and validated downstream fields; it does not expose the controller's unrestricted `tools/call` interface or a shell.

The existing Workspace controller remains the sole owner of device leases, software inventories, worker identities and workspace lifecycle. The privileged bridge does not create shadow hardware or software stores. Environment builds may be asynchronous and should be checked through the owning Workspace operation-status API; an accepted request does not prove that software has finished installing. The privileged executor returns an audit correlation ID and an explicit `state`: `accepted` for Workspace, systemd and Kubernetes submissions, or `completed` for release activation and reversion after host-local release checks. The unified MCP response reports `accepted: true` for submissions and reserves `executed: true` for verified release transitions. An audit ID is not a downstream Workspace operation ID, and accepted submissions must be checked through their owning service before treating the work as finished. Delegated tool responses must contain a matching JSON-RPC identifier and the required MCP content array. An omitted `isError` is treated as `false`, as defined by MCP; explicit failures and malformed responses are rejected. This delegation is disabled by default and requires trusted controller configuration and deployment validation.

## Durable privileged request identities

The private executor's `POST /v1/execute` JSON body contains
`request_id`, `operation`, `target` and `arguments`. The request ID is
exactly 32 lowercase hexadecimal characters. A caller must generate a unique
ID before the first attempt and retain it across timeouts or lost responses.
All privileged MCP and native tool calls require `request_id`. The gateway
does not include that field in the owner's canonical argument digest.

The protected append-only audit journal records the request ID as the audit
event ID before any side effect. Authorization, operation admission and
exact operation/target/argument binding are verified independently. A
concurrent request with the same ID cannot start another mutation. Once
recorded, the ID remains reserved permanently, even after a recovery
operator has reconciled an uncertain outcome.

Retries with a matching ID must still provide the signed owner assertion
and an active authorized owner session. The security authority's private
`POST /v1/replay` operation performs a read-only policy check without
issuing a new destructive confirmation or credential ticket. A matching
`accepted` or `succeeded` journal record is returned as the original
`accepted` or `completed` result with the same `audit_id` and
`replayed: true`. A `started`, `failed`, `indeterminate` or
`reconciled` record never initiates another effect under that identity.
Investigate uncertain operations through the trusted audit recovery workflow
before initiating a separately approved action under a different request ID.

Durable fleet releases use a transaction-derived request ID for every host
activation or rollback, preserving the same host-execution identity across
coordinator restarts. Hosts without an identity-preserving interface are not
eligible for durable fleet activation.

## Credential custody and rotation

The owner-only socket can list opaque credential handles, store provider credentials, revoke entries, and atomically rotate an existing entry. Each add, delete, or rotate operation requires a fresh administrative password and an unreplayed TOTP submitted directly to the trusted owner channel. The gateway does not expose any credential-inventory or plaintext-credential operation.

Use the local administrative command `homelab-admin credential-rotate PROVIDER OPAQUE_HANDLE` on a trusted terminal. The program reads the replacement credential and owner factors through `systemd-ask-password` without putting them in shell history or command arguments. It sends the replacement only over the owner-only Unix socket. Successful rotation retains the provider and opaque handle but encrypts the replacement under a fresh nonce and changes the credential's revision. Previously prepared scoped credential capabilities are tied to the prior revision and fail on use after rotation; a new capability requires another current owner authorization.

Provider-specific OAuth refresh and authorization-code exchange remain internal source-library operations until a configured provider runtime and independently protected browser/device flow are integrated. Do not send tokens or administrator factors to an MCP request.

## ChatGPT-initiated browser authentication

The platform MCP tool `security_session_open` returns the configured HTTPS sign-in URL and current non-secret session status. ChatGPT can present that URL as a link or an external-open action. The user enters the owner password and six-digit TOTP on the separate trusted page. Neither the credential form nor a reusable administrator token is part of an MCP tool argument or result. Once authenticated, the user can return to ChatGPT and retry an allowed operation. The MCP also exposes session status and immediate closure.

The optional `services.homelabSecurity.browser.enable` NixOS setting publishes the credential portal through a localhost-only Nginx listener and a separate Unix socket accessible to the Nginx group. The default hostname is `auth.dbontr.me`. **Before enabling it**, create a dedicated Cloudflare Access application for that exact hostname, with an owner-only identity policy, and set `services.homelabSecurity.browser.audience` to that application's verified audience. The tunnel validates the signed Access assertion before proxying, and the NixOS module requires an audience to be configured. Configure `services.homelabPlatform.securityLoginURL = "https://auth.dbontr.me"` only when the portal is reachable and access denial has been tested.

The credential-entry page is a top-level browser destination opened from ChatGPT, rather than an ordinary ChatGPT form. It rejects foreign Host and Origin headers, requires short-lived single-use CSRF values, disables framing and caching, uses no third-party JavaScript, and displays the exact destructive-operation digest before confirmation. A separate Unix socket and operating-system group prevent the MCP gateway from reaching the owner form directly. The browser proxy can reach only the restricted portal endpoint; it cannot access the owner CLI socket or ordinary executor credentials.

Do not let the credential portal share a Cloudflare Access application with the MCP gateway, accept broad email-domain rules, or bypass authentication for preflight/health routes. The application is disabled by default. Never enable the public hostname until the Access identity policy and origin JWT validation are verified using both authorized and unauthorized sessions.

## Verified identity and executor authority

The authenticated owner session is bound to the verified Cloudflare Access principal (issuer, subject and owner email). The browser portal and platform MCP must use **different application audiences**. The independent security process verifies each Access JWT signature against locally installed JWKS keys and checks issuer, audience, expiry, subject and the configured owner email. It rejects unverified identity headers and service-token assertions without human identity. JWT signing keys are installed by the operator outside Git and updated through a trusted change process; stale signing keys fail closed and require coordinated rotation. The browser form, MCP gateway and privileged executor must forward the original signed assertion, never an asserted email value.

The security service stores the identity binding and all confirmations internally. Requests from another principal cannot inspect, use or revoke that session. Legacy local terminal logins have a separate local principal and never unlock the ChatGPT gateway. An active session is not an unrestricted capability token.

The `homelab-executord` process holds local operating-system authority and permits typed, allowlisted service start, stop, reload and restart actions, Kubernetes deployment operations, and protected NixOS generation activation/reversion. Its optional mutual-TLS host endpoint can stage a clean, exact Git revision without activating it. Every privileged mutation requires an owner-only audit journal. Missing audit custody prevents executor startup or execution, and a journal append failure prevents the side effect. All side effects have distinct started and outcome records. Request submissions receive `accepted` outcomes, while a verified release transition receives `succeeded`. An interrupted record must be treated as an uncertain outcome.

Kubernetes deployment scaling can include an exact `resource_version` string (1–128 characters, beginning with a letter or digit and containing only letters, digits, underscores, periods or hyphens) in the owner-reviewed canonical arguments. The executor forwards this as kubectl's server-validated `--resource-version` precondition and the Rust actuator validates it again when selected. A changed resource version causes the scale request to fail rather than silently modifying a different object revision. Without a supplied version, scale retains the existing allowlist-only behavior. Rollout restart has no equivalent kubectl resource-version option and requires additional versioned patch semantics before claiming the same guarantee.

An optional Rust actuator enforces an additional typed command boundary after independent authorization and a durable pre-execution audit. The operator provides immutable service and Kubernetes allowlists through Nix; the actuator rejects any operation not appearing in those exact lists, any dynamic command selection, excess replicas, or dangerous system services. It runs fixed systemd/kubectl argv without a shell. This option remains disabled unless explicitly selected, and the Rust binary must pass its offline source checks and host acceptance before the operator enables it. The Go security daemon remains the only owner of credentials and active sessions. The executor starts the actuator in a separate Linux process group and cancels that group when its operation context ends, so an inherited systemctl or kubectl subprocess does not continue after the caller stops the request. Cancellation cannot reverse changes already accepted by systemd or the Kubernetes API; release and workload state must be inspected separately.

The executor refuses network, security, orchestration, monitoring and media-critical service names even if included in an administrator configuration. Each lifecycle mutation requires an explicit allowlisted unit, exact host and arguments, independent owner authorization, and a one-time destructive confirmation; arbitrary systemctl flags and unit files are never accepted.

 It has no arbitrary shell, custom binary, filesystem write or Kubernetes root command API. Each request is checked by `homelab-securityd` using the independently verified principal and exact canonical arguments. The executor socket permits only the narrow `homelab-executor-client` group. The MCP gateway is forbidden from carrying the Workspace root token when this path is configured, and its privileged backend tool forwarding is disabled until that backend supports executor-side enforcement.

Configure `services.homelabSecurity.access` with the issuer, gateway application audience, protected owner email file, and pinned JWKS file. Set `services.homelabSecurity.browser.audience` to the **distinct** dedicated browser Access application audience. The platform's `securityClientSocket` and `executorSocket` must point to the protected services. The local executor is opt-in with `services.homelabExecutor.enable`; its operation and resource policy is independent from the gateway.

Generation activation from the executor also requires `services.homelabExecutor.allowReleaseActivation = true` and a one-use destructive confirmation. The default is false. The release controller stages only clean exact Git commits, uses offline Nix builds, records the active and previous store paths, checks specified systemd services, and attempts rollback on activation failure. Interrupted transactions block further activation until local recovery. It does not automatically deploy to another host or alter the public firewall.

## Credential vault and scoped secret use

The security service may load an owner-controlled 32-byte AES-GCM vault key through `services.homelabSecurity.vaultKeyFile` and systemd `LoadCredential`. The key is never stored in Git or read by the gateway. On a trusted local terminal, `homelab-admin vault-init --key-file PATH` creates the encryption key in a private directory. The same CLI supports `credential-add PROVIDER`, `credential-list` and `credential-delete HANDLE`. Enrollment and deletion require a fresh password and TOTP. The protected owner socket returns opaque credential handles, not secret values.

The `ScopedCredentialBroker` stores short-lived capabilities that bind the verified principal, provider credential, operation, target, canonical argument digest, and expiration. A trusted executor callback receives plaintext only after an independent authorization decision; the capability is consumed before the callback and cannot be replayed. It does not allow arbitrary MCP clients to retrieve vault contents. Supported vendor OAuth exchanges use the existing PKCE broker. Software-specific login adapters must still enforce their provider's scopes and token lifecycle before they are made available to production workers.

## Authoritative provider credential vault

The independent owner vault is the authoritative store for operator-managed provider credentials. GitHub and GitLab can read short-lived plaintext values directly from that store on each provider request, rather than maintaining a second persistent PAT in the compute controller. This mode requires explicit operator configuration and must not be enabled until the private TLS route and certificate identities are validated.

A named service must have a dedicated X.509 client certificate whose sole DNS SAN matches one entry in `services.homelabSecurity.remote.allowedClientDNS`. Set `services.homelabSecurity.remote.serviceCredentialGrants` to bind that `clientDNS`, an `alias`, a `provider`, and the existing opaque vault `handle`. No secret value belongs in Nix configuration. The security daemon verifies that the handle exists and belongs to the named provider before enabling the grants. The service credential route is accessible **only** through the separately configured, private TLS 1.3 listener, with mandatory client certificates and server trust verification; it is not registered on the owner, browser or platform gateway sockets.

The Compute controller accepts trusted deployment settings `PWM_VAULT_URL`, `PWM_VAULT_SERVER_NAME`, `PWM_VAULT_CA_FILE`, `PWM_VAULT_CLIENT_CERT_FILE`, `PWM_VAULT_CLIENT_KEY_FILE`, and one or both of `PWM_VAULT_GITHUB_ALIAS` and `PWM_VAULT_GITLAB_ALIAS`. The controller verifies the pinned server identity and private CA trust and fetches the provider credential at the instant of each authorized provider call. Vault rotation and deletion therefore affect subsequent calls without recreating a permanent provider PAT file. The controller receives plaintext only for the necessary outbound provider request and never returns it through project tools or worker operations. A failed vault request denies provider access; no alternate credential is silently selected.

The Helm Compute controller supports `controller.vault.enabled` with an approved HTTPS endpoint, expected server name, CA certificate Secret, client identity Secret, and provider aliases. These Kubernetes Secrets contain only mTLS *bootstrap trust and client key material*; they do not contain GitHub or GitLab PATs. A configured provider vault alias cannot coexist with a separately mounted PAT for the same provider. Operators should ensure that client certificate identity is exclusive to this service and that the private security listener is never published through the public MCP route.

Central authorization and central **storage** do not mean that every secret can have one physical copy. The vault's own encryption key, password verifier, TOTP seed, mTLS private keys, worker bootstrap credentials, backup recovery keys and other independent trust roots necessarily remain separately protected. Other integrations require their own explicit credential-grant mappings, and configuring vault access does not reconcile Kubernetes Secrets or automatically remove legacy token files. Credential delivery is a trusted-service operation, not a generic model-controlled secret read API.

## Credential consumers and authority

Provider tokens remain encrypted at the independent owner vault. Each consuming service has its own verified TLS client certificate and an exact `clientDNS`, `alias`, `provider`, and `handle` binding in the security daemon's `remote.serviceCredentialGrants`. Those grants control which service may read which record; they do not create broad permission to enumerate vault entries. The secrets are obtained at the instant a provider request is made. A revoked vault handle or unavailable vault fails closed.

The platform uses `services.homelabPlatform.vault` and the specific `vaultAlias` for Seerr and purpose-specific model completions. Each token requires an independently authorized owner operation and an exact request-digest or media-API scope. The selected route stores only the alias and endpoint; secrets never become model arguments. Federated Workspace, Learning and Health MCP discovery use separately restricted service credentials loaded from protected files, since discovery and health checks cannot mint human owner tickets. The main platform never receives the Compute administrator token, the owner password, or the privileged executor credentials.

Engineering runs are autonomous and outlive the initiating request. The daemon therefore uses independent, protected service-token files for its project-scoped Compute editor and optional authoring model, not an expired or fabricated human owner ticket. Its Compute identity remains non-administrator, and the controller verifies the current principal and exact project grants. The engineering process remains separate from the platform gateway.

Compute uses `services.workspaceMcp.vault` for GitHub, GitLab, or consented embeddings, with `githubAlias`, `gitlabAlias`, and `embeddingsAlias` independently granted. Its NixOS release renders only alias and certificate Secret names into the controller chart. The embedding client requests a credential only on an explicitly permitted text-embedding request. A credential failure never reroutes the text to a different provider.

Every service still needs independently protected bootstrap material: the vault unlock key, mTLS client private key, TLS CA, service identity, local console verifier, and scoped worker credentials. Those should not be transferred through model input, application logs, or repository content. The root vault is not an unrestricted secret accessor, and no process may obtain another service's alias merely by changing a tool argument.

## Executor credential use

The independent `homelab-executord` holds an exact, operator-configured allowlist for Workspace projects, workers, images, software, and packages. `services.homelabExecutor.workspace` requires one credential authority: an owner-protected `rootTokenFile`, or a `vaultAlias` bound to the executor's remote-authorization client certificate. The alias mode requires `services.homelabExecutor.authorization.remote.enable`, which verifies owner decisions over pinned mutual TLS. The gateway never receives this credential.

Before a Workspace operation, the executor validates the local host, operation, arguments, and resource allowlist. It asks the independent security service to authorize the exact canonical operation. The executor then writes a durable audit-start record. Only after these steps may the Workspace bridge resolve the current credential and invoke the fixed downstream tool. A revoked or rotated credential takes effect on the next request; an unreachable vault, absent credential, or ambiguous credential authority causes denial. An outcome audit record follows the downstream call.

The executor does not provide a generic HTTP proxy, a general command runner, or credential-enumeration operations. The remote authorization mTLS identity must have an explicit service credential grant for the exact `vaultAlias`; the vault does not infer a credential from the requested Workspace operation. Credentials for bootstrap TLS identities, Nix activation and Kubernetes access remain protected independently.

## Kubernetes deployment operations

The optional `services.homelabExecutor.kubernetes` policy permits only exact namespace/deployment pairs listed by the operator. The separate executor receives a root-controlled kubeconfig through systemd credentials and exposes `cluster__deployment_restart` and `cluster__deployment_scale`. Commands are constructed from fixed `kubectl` arguments without shell interpretation. Scaling is bounded by `maxReplicas`, and both operations require an active owner identity and a separate destructive-operation confirmation. The executor does not expose a Kubernetes administrator shell or arbitrary apply/delete endpoint.

## Enforcement boundary

The gateway can query authorization and request revocation but cannot authenticate the owner, approve a destructive operation, issue a session credential, change the configured policy, or extend the absolute lifetime. Socket access relies on operating-system user and group isolation. The daemon validates the configured operation and target for every request, canonicalizes arguments with duplicate-key rejection, and fails closed if the service is unreachable.

**Authorization checks alone do not secure an executor that still accepts a separate, broadly privileged credential.** Downstream Workspace and cluster mutation paths must use independently enforced resource permissions and grant checks at their actual executor boundaries. Until that migration is complete, this service must not be described as authorizing unrestricted cluster administration. Direct privileged backend access from the gateway would bypass the local approval decision.

Before activation, verify socket ownership, wrong-password and TOTP rejection, replay after restart, idle and absolute expiry, immediate revocation, deliberate confirmation, concurrent confirmation consumption, unauthorized resource substitution, invalid JSON, and gateway-compromise resistance. Keep an out-of-band management path through NanoKVM during a fleet rollout. These controls are not evidence that a NixOS generation has been deployed or that a real backend has accepted an operation.
