# Git and Provider Workflows

Use this reference for source control, GitHub Issues, PRs, Projects V2, and locally hosted Git. Read the repository's established conventions first. Use provider-specific APIs only when exposed and authorized.

## Planning is independent of code and knowledge

Keep separate concepts:

| Authority | Intended contents | Suitable provider |
| --- | --- | --- |
| Source Git | Code, tests, environment definitions, release tags | GitHub; approved local bare Git; verified remote Git adapter |
| Knowledge Git | Decisions, findings, experiments, source manifests and handoffs | Same Git repo or a selected separate Git repo |
| Planning | Issues, owners, status, priorities, milestones, PR reviews | GitHub Projects V2/Issues, or another explicitly supported system; optional |
| Artifact storage | Large verified outputs and original binaries | Content-addressed object/artifact service, verified Git LFS where authorized |

A project can use **local Git for code and knowledge** and a separate GitHub repository for Issues/Projects. Link that actual GitHub repository with role `planning`. This does not make local branches GitHub pull requests. A PR against the planning repository cannot be treated as a review of code that exists only on another Git remote; use verified local Git commits and the chosen local peer-review workflow instead.

## Native GitHub capabilities across repositories

A Workspace project and a GitHub Projects V2 board are independent objects. A board may coordinate Issues and PRs from **several** repositories; each source repo owns its branches, checks, rules and releases. Use its immutable GitHub Project node ID, not a guessed URL, and attach a board only after an owner has selected it. `github_project_board`, `github_project_items`, `github_project_fields`, `github_project_add_item`, `github_project_add_draft` and `github_project_set_status` have distinct authority and prerequisites.

Prefer the supported native GitHub feature rather than a duplicate Markdown registry:

- **Issues:** use correct repository, labels, types, milestones, assignees, dependencies and sub-issues when available. Do not create redundant cross-repository copies; link related issues.
- **Projects V2:** use native fields, item status, iterations, linked issue/PR entries and views, preserving field option IDs and board permissions. GitHub owns live planning status.
- **Pull requests:** use branch rules, requested reviewers, review comments, protected merge checks, merge queue where enabled, and verified post-merge branch cleanup. Never fabricate a passed check or self-approve to bypass policy.
- **Actions and security:** inspect actual workflow runs, check suites, required status checks, dependency alerts and code-scanning results where accessible. If GitHub Actions is unavailable, run authorized alternative tests but do not claim the native check passed.
- **Releases and packages:** use tags and GitHub Releases for distributed source/binary releases, GitHub Packages and GHCR for published packages/images, and immutable image digests for deployment. Confirm package/repository permissions and visibility separately; package access is not implied by linking a repository.
- **Discussions, wiki and documentation:** use only for genuine long-form discussions or maintained product documentation, not as an alternative secret or knowledge store.

For capabilities absent from the current Workspace MCP toolset, use an explicitly connected and authorized native GitHub tool, or report the unimplemented operation. Do not fake GitHub semantics with an untracked local file or unapproved command. Any account-wide operation requires its separate authorization; ordinary project linkage is not permission to mutate all repositories.

## GitHub issue quality

Create an issue only for an actionable unit that is not already represented by an existing issue. Use a precise title and concise body identifying:

- Problem or outcome and why it matters.
- Scope and non-goals.
- Acceptance criteria that can be checked against actual results.
- Relevant project/repository links, dependencies, security or hardware constraints.
- Owner, labels, type, priority or milestone only when those fields exist and carry useful meaning.

Use parent/sub-issues for genuine decomposition, not for every trivial step. Reference source commits or knowledge records when appropriate; do not duplicate an entire research notebook in an issue comment. Avoid tickets that exist solely to record a chat exchange.

## GitHub Projects V2

- Discover an existing board, project ID, fields and view configuration; do not guess GraphQL node IDs or invent dropdown option IDs.
- Keep one authoritative status field rather than independent copies across Issues, Project items and knowledge notes. Native issue/PR fields should remain authoritative where GitHub exposes them.
- Prefer a minimal workflow such as Backlog, Ready, In progress, In review, Blocked, Done **only when the owner chooses that configuration**. Do not replace an established board's values without authorization.
- Use project items to reference genuine Issues or PRs. Use draft items for early planning only when beneficial; convert to trackable work when scope is concrete.
- Use views for working backlog, delivery work and release/milestone overview when they answer a real question. Avoid redundant fields and decorative dashboards.
- On completion, update the canonical work item after verifying the associated Git commit and acceptance tests. Do not close work merely because a PR exists.

## Branches, commits and pull requests

1. Confirm source repository, default/target branch, branch protections, expected head SHA and required checks.
2. Use an existing branch or a short meaningful branch name such as `fix/lease-ownership` or `feat/knowledge-git`. Avoid chronological names, duplicate staging branches and branches with mixed unrelated work.
3. Make cohesive, reviewable commits under the actual authorized author identity. Explain enduring technical intent, not the conversation that led to it. Do not add bot co-author trailers or misleading attribution. Obey required signatures and organization audit policy.
4. Verify format, unit tests, integration checks, security controls and relevant live acceptance. Record what was *not* tested. Check the diff for secrets, local machine paths, abandoned files and generated noise.
5. Open a PR only against the correct source repository and branch. Include a technical summary, affected contract, test evidence, operational risks, migration requirements and linked issue if relevant. Keep drafts for unverified or incomplete work.
6. Obtain required review and approvals. Do not infer approval or bypass rules. Merge by the repository's authorized policy only after checks and source provenance are verified. Delete the branch when safe and required by workflow; keep recovery references where needed.

GitHub can link a PR to an issue. Closing-keyword automation depends on the target branch and provider semantics; do not assume any PR merge closes the issue. Verify actual issue state after merge.

## Local Git fallback

Native Git provides objects, commits, branches, tags and transports; it does **not** provide a hosted issue tracker, GitHub Projects V2 board or native GitHub pull-request reviews. When those operations are requested without a compatible provider:

1. Continue permitted Git reads, builds and publication.
2. Return a precise unavailable-capability result for Issues/PRs/boards, not a fake success.
3. Offer an explicitly authorized provider integration or independent planning repository; do not create one silently.
4. Preserve expected-head checks and verify publication against the configured authority. A successful local commit without an independently verified remote update is not a successful push to another server.

A local bare Git repository must have operator-controlled access, stable storage, backups and tested restoration. Never assume GitHub's retention or audit features are available locally.

## Repository identity and renames

Keep the Workspace project ID separate from GitHub repository IDs and names. For every link verify GitHub's numeric repository `id`, current `full_name`, default branch and role; preserve that ID in the project's private authority record or authorized registry. GitHub redirects after rename are a convenience, not a stable reference: an old name can be reused and GitHub-hosted Actions references do not automatically redirect.

On rename or transfer: verify the destination has the **same repository ID**, check active workspaces and naming conflicts, use the controller's authorized `github_account_rename`/`github_account_transfer` or `github_account_reconcile`, then re-read project membership, library source locators, knowledge destinations and checked-out workspaces. Preserve pinned historical commit citations. External changes are not assumed to reconcile without explicit action and verification. Do not rename a project's knowledge repository merely because one linked source repo changed name.

## Read-only discovery versus account mutations

Project operations should normally be restricted to linked repositories. Account-wide search, rename, transfer, delete, branch protection changes and settings changes require explicit authority and exact target identities.

Account-level archive/unarchive and supported repository settings are owner-only MCP operations. They use immutable repository-ID checks, explicit confirmation, administrator permission, provider read-back and audit records. Settings outside the supported typed configuration fields require separately implemented and verified tools. Never represent an unimplemented GitHub administration capability as available. Confirm immutable repository ID and destination before renaming/transferring; reconcile all links and check whether active workspaces still depend on the old repository name. Do not confuse repository display names with stable Git commit identities.

## Sources

- [GitHub Projects overview](https://docs.github.com/en/issues/planning-and-tracking-with-projects/learning-about-projects/about-projects)
- [GitHub Projects best practices](https://docs.github.com/en/issues/planning-and-tracking-with-projects/learning-about-projects/best-practices-for-projects)
- [GitHub Projects fields](https://docs.github.com/en/issues/planning-and-tracking-with-projects/understanding-fields)
- [GitHub Issues creation](https://docs.github.com/en/issues/tracking-your-work-with-issues/using-issues/creating-an-issue)
- [Linking pull requests to issues](https://docs.github.com/en/issues/tracking-your-work-with-issues/using-issues/linking-a-pull-request-to-an-issue)
- [GitHub repository rulesets](https://docs.github.com/en/repositories/configuring-branches-and-merges-in-your-repository/managing-rulesets/about-rulesets)
