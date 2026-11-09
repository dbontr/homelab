# Isolated engineering acceptance

The `homelab-engineering-acceptance` command checks the project-scoped
engineering workflow without enabling the Homelab gateway, independent
engineering daemon, or any NixOS service. It uses the same compute client,
model adapter, workflow engine, private store, and local Unix-socket lifecycle
interface as the engineering service.

The command creates at most one isolated workspace in an approved project,
allows one source file under `tests/engineering-acceptance/`, executes a
fixed marker check, exports a verified artifact, and prepares a GitHub **draft**
pull request against an exact 40-character base commit. It never merges,
changes infrastructure, or activates a release.

## Prerequisites

- A repository linked to an existing compute project, with an approved
  `debian-bookworm-slim` image on an eligible worker.
- A non-administrator OIDC bearer credential issued by the controller's
  configured identity provider. The service principal must have exactly the
  project's `editor` grant. The controller exposes only the authenticated
  principal's own role through `GET /api/projects/{id}/role`. Do not use the controller owner or administrator token.
- A compatible chat-completions model endpoint over HTTPS (or loopback HTTP)
  and a configured model name. If the provider requires authentication, place
  its bearer credential in a separate private regular file.
- Access to the pinned GitHub repository and permission for the compute
  controller to create draft pull requests. The controller still enforces
  repository membership and the expected base commit.

Store token files outside the repository, with mode `0600` or stricter.
Do not pass tokens in chat messages, command arguments, logs, or Git commits.

## Preflight and execution

From the root of an authorized source checkout:

```sh
cd services/core
export HOMELAB_COMPUTE_URL="https://compute.example.invalid/mcp"
export HOMELAB_COMPUTE_TOKEN_FILE="/path/to/private/oidc-project-editor-token"
export HOMELAB_ENGINEERING_MODEL_URL="https://model.example.invalid/v1/chat/completions"
export HOMELAB_ENGINEERING_MODEL_NAME="configured-model-name"
# Optional when the model provider requires a bearer token:
export HOMELAB_ENGINEERING_MODEL_TOKEN_FILE="/path/to/private/model-token"

go run ./cmd/homelab-engineering-acceptance -revision "40-character-main-commit"
go run ./cmd/homelab-engineering-acceptance -revision "40-character-main-commit" -execute
```

Replace all placeholders before running. The first command verifies the OIDC identity, exact project-editor role,
linked source repository, and model decision format without scheduling a
workspace. The second command authorizes one bounded run. The source commit
must equal the repository's current `main` HEAD; if `main` changes, draft
publication fails closed.

The acceptance runner reserves one CPU and 1024 MiB in the configured image
for a time-limited workspace. It uses a private temporary directory for
durable workflow state and a local Unix socket. Preserve that directory until
any failed or interrupted run has been reconciled. The command requests
workspace teardown when it exits and waits for a confirmed terminal state.
An unconfirmed cleanup causes acceptance to fail; inspect and stop the
workspace through the controller before reattempting. Resource leases also
have a finite expiry.

A successful result includes the draft pull-request URL and a confirmed base
SHA. Inspect the draft's changed-file list before closing it without merging.
The result does not establish the correctness of unrelated software,
deployment behavior, or hardware acceleration.

## Acceptance criteria

An acceptance run passes only when the non-administrator identity and model
adapter pass preflight, the exact source revision is checked out, all owner
checks pass after the final file edit, an immutable project artifact matches
the tested source digest, and GitHub confirms a draft pull request against the
pinned commit. The worker workspace must be confirmed stopped or absent from the controller's
workspace listing.

Failed model decisions, missing grants, stale commits, conflicting file
digests, uncertain operations, or a denied cleanup are failures; they must not
be presented as successful acceptance.
