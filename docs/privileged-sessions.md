# Privileged owner sessions

The independent `homelab-securityd` process owns password verification, the TOTP secret, session state, and confirmation decisions. Its default configuration binds no TCP listener. The optional remote authorization listener uses mutual TLS and exposes only authorization decisions to allowlisted executor certificate identities. Its owner socket is mode 0600. Its gateway socket is mode 0660 with membership restricted to `homelab-security-client`. A separate systemd service account holds the credential files, and the platform gateway must not receive them.

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

The `homelab-executord` process holds local operating-system authority and permits typed, allowlisted service start, stop, reload and restart actions, Kubernetes deployment operations, and protected NixOS generation activation/reversion. Its optional mutual-TLS host endpoint can stage a clean, exact Git revision without activating it.

The executor refuses network, security, orchestration, monitoring and media-critical service names even if included in an administrator configuration. Each lifecycle mutation requires an explicit allowlisted unit, exact host and arguments, independent owner authorization, and a one-time destructive confirmation; arbitrary systemctl flags and unit files are never accepted.

 It has no arbitrary shell, custom binary, filesystem write or Kubernetes root command API. Each request is checked by `homelab-securityd` using the independently verified principal and exact canonical arguments. The executor socket permits only the narrow `homelab-executor-client` group. The MCP gateway is forbidden from carrying the Workspace root token when this path is configured, and its privileged backend tool forwarding is disabled until that backend supports executor-side enforcement.

Configure `services.homelabSecurity.access` with the issuer, gateway application audience, protected owner email file, and pinned JWKS file. Set `services.homelabSecurity.browser.audience` to the **distinct** dedicated browser Access application audience. The platform's `securityClientSocket` and `executorSocket` must point to the protected services. The local executor is opt-in with `services.homelabExecutor.enable`; its operation and resource policy is independent from the gateway.

Generation activation from the executor also requires `services.homelabExecutor.allowReleaseActivation = true` and a one-use destructive confirmation. The default is false. The release controller stages only clean exact Git commits, uses offline Nix builds, records the active and previous store paths, checks specified systemd services, and attempts rollback on activation failure. Interrupted transactions block further activation until local recovery. It does not automatically deploy to another host or alter the public firewall.

## Credential vault and scoped secret use

The security service may load an owner-controlled 32-byte AES-GCM vault key through `services.homelabSecurity.vaultKeyFile` and systemd `LoadCredential`. The key is never stored in Git or read by the gateway. On a trusted local terminal, `homelab-admin vault-init --key-file PATH` creates the encryption key in a private directory. The same CLI supports `credential-add PROVIDER`, `credential-list` and `credential-delete HANDLE`. Enrollment and deletion require a fresh password and TOTP. The protected owner socket returns opaque credential handles, not secret values.

The `ScopedCredentialBroker` stores short-lived capabilities that bind the verified principal, provider credential, operation, target, canonical argument digest, and expiration. A trusted executor callback receives plaintext only after an independent authorization decision; the capability is consumed before the callback and cannot be replayed. It does not allow arbitrary MCP clients to retrieve vault contents. Supported vendor OAuth exchanges use the existing PKCE broker. Software-specific login adapters must still enforce their provider's scopes and token lifecycle before they are made available to production workers.

## Kubernetes deployment operations

The optional `services.homelabExecutor.kubernetes` policy permits only exact namespace/deployment pairs listed by the operator. The separate executor receives a root-controlled kubeconfig through systemd credentials and exposes `cluster__deployment_restart` and `cluster__deployment_scale`. Commands are constructed from fixed `kubectl` arguments without shell interpretation. Scaling is bounded by `maxReplicas`, and both operations require an active owner identity and a separate destructive-operation confirmation. The executor does not expose a Kubernetes administrator shell or arbitrary apply/delete endpoint.

## Enforcement boundary

The gateway can query authorization and request revocation but cannot authenticate the owner, approve a destructive operation, issue a session credential, change the configured policy, or extend the absolute lifetime. Socket access relies on operating-system user and group isolation. The daemon validates the configured operation and target for every request, canonicalizes arguments with duplicate-key rejection, and fails closed if the service is unreachable.

**Authorization checks alone do not secure an executor that still accepts a separate, broadly privileged credential.** Downstream Workspace and cluster mutation paths must use independently enforced resource permissions and grant checks at their actual executor boundaries. Until that migration is complete, this service must not be described as authorizing unrestricted cluster administration. Direct privileged backend access from the gateway would bypass the local approval decision.

Before activation, verify socket ownership, wrong-password and TOTP rejection, replay after restart, idle and absolute expiry, immediate revocation, deliberate confirmation, concurrent confirmation consumption, unauthorized resource substitution, invalid JSON, and gateway-compromise resistance. Keep an out-of-band management path through NanoKVM during a fleet rollout. These controls are not evidence that a NixOS generation has been deployed or that a real backend has accepted an operation.
