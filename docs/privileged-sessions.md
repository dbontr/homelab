# Privileged owner sessions

The independent `homelab-securityd` process owns password verification, the TOTP secret, session state, and confirmation decisions. It binds no TCP listener. Its owner socket is mode 0600. Its gateway socket is mode 0660 with membership restricted to `homelab-security-client`. A separate systemd service account holds the credential files, and the platform gateway must not receive them.

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

## Enforcement boundary

The gateway can query authorization and request revocation but cannot authenticate the owner, approve a destructive operation, issue a session credential, change the configured policy, or extend the absolute lifetime. Socket access relies on operating-system user and group isolation. The daemon validates the configured operation and target for every request, canonicalizes arguments with duplicate-key rejection, and fails closed if the service is unreachable.

**Authorization checks alone do not secure an executor that still accepts a separate, broadly privileged credential.** Downstream Workspace and cluster mutation paths must use independently enforced resource permissions and grant checks at their actual executor boundaries. Until that migration is complete, this service must not be described as authorizing unrestricted cluster administration. Direct privileged backend access from the gateway would bypass the local approval decision.

Before activation, verify socket ownership, wrong-password and TOTP rejection, replay after restart, idle and absolute expiry, immediate revocation, deliberate confirmation, concurrent confirmation consumption, unauthorized resource substitution, invalid JSON, and gateway-compromise resistance. Keep an out-of-band management path through NanoKVM during a fleet rollout. These controls are not evidence that a NixOS generation has been deployed or that a real backend has accepted an operation.
