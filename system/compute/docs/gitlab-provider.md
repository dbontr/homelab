# GitLab repository provider

Eminence supports GitLab as a source and project-knowledge authority through a controller-held personal access token (PAT). The GitLab adapter is independent of GitHub and local bare Git.

## Credentials

Set `PWM_GITLAB_PAT_FILE` to an absolute path containing a GitLab PAT. The file must be regular, non-world-accessible, non-group-writable, and at most 4 KiB. The controller loads the token at startup and never returns it in native API or MCP responses. The default API endpoint is `https://gitlab.com/api/v4`. A trusted operator may configure `PWM_GITLAB_API_URL` with an exact HTTPS origin ending in `/api/v4` for a self-managed GitLab instance; a tool caller cannot select a different origin.

A Kubernetes controller can use `controller.gitlabPATSecretName` in the Helm chart. The Secret must contain a `token` key and is mounted read-only as a service-group-readable file. The chart never embeds the token in its values or workload environment. Provision that Secret through an authorized secret-delivery mechanism; do not commit the PAT to this repository, pass it through ChatGPT, or place it in a project-knowledge document. Keep token scope and lifetime minimal, restrict membership in each GitLab project, and rotate the token at its issuing account. GitLab's repository-files API requires a PAT with `api` scope for writes and accepts `read_api` or `read_repository` for reads; avoid `api` when writes are not needed.

## Repository identities and capabilities

Use an explicit repository locator such as `gitlab:group/repository` or `gitlab:group/subgroup/repository`. The controller verifies the provider-reported project identity before linking a GitLab repository. A project may link GitHub and GitLab sources independently; a GitHub-only planning repository is still required for GitHub Projects, Issues and pull-request operations.

The provider supports:

- Exact commit lookup from a branch, tag or commit identifier.
- Reading a file at a pinned commit with a bounded response and a content SHA-256.
- Importing a pinned `tar.gz` archive into an approved project workspace.
- Selecting a GitLab repository as a revisioned project-knowledge store.
- Creating or replacing one project-knowledge file with explicit expected branch revision, per-file `last_commit_id` protection when the file exists, and post-write readback.
- Source operations through the existing project-scoped `git_*` MCP methods.

GitLab's file-update API does not expose an atomic compare-and-swap on the whole branch head. A preflight head check and the per-file `last_commit_id` prevent stale known-file overwrites, but an unrelated concurrent branch update can be included in the resulting head. The response declares that guard explicitly; use a protected branch and normal provider review policies for consequential changes. Do not treat a readback as proof that no concurrent changes occurred elsewhere.

GitLab source access is independent of the GitHub-only issue, merge-request, project-board and account-administration methods. GitLab-specific workflow tools require separate permission-aware implementations; the `git_*` source tools are not aliases for unrestricted provider administration. A GitLab archive redirect to an unapproved origin is rejected. The adapter does not authorize a controller PAT to run in a compute worker, engineering model, browser or platform gateway.

## Secret ownership

The independent owner-encrypted credential vault is the authoritative store for provider PATs when the optional mTLS credential mode is enabled. The controller can read GitLab credentials directly from that vault using a trusted service identity, a pinned private HTTPS endpoint, and an operator-defined alias. Configure the exact `controller.vault.gitlabAlias` and the `controller.vault` trust options; do not set `controller.gitlabPATSecretName` simultaneously. The controller retrieves the current PAT on each GitLab API request, so owner rotation and revocation take effect on subsequent calls. The separate read-only PAT Secret remains a supported alternative when the vault client is not enabled. TLS client private keys and vault unlock material remain protected bootstrap credentials, and automatic migration of previously created Kubernetes Secrets is not performed.
