# GitHub workflow

## Establish repository state

1. Resolve the exact owner/repository and the canonical project; prefer linked repositories over guessing by name.
2. Read the default branch, latest SHA, repository-specific conventions, root references, open issues, open pull requests and existing implementation.
3. Create a task-specific branch from the confirmed base. Do not push directly to protected main; avoid force updates.
4. Keep coherent changes together and update every affected test, contract, documentation and migration. Re-read the diff before publication.

## Issues and project organization

A project may own multiple repositories and link one or more shared research libraries. Use the project's native record and reference identifiers, not a shadow store of copied tasks or repos. Link dependent issues using explicit relations and acceptance criteria. Set a clear scope, risk classification, ownership, expected outputs, dependencies and verification method. Do not create gratuitous tickets for work already completed in a coherent change.

## Changes and publication

- Pin the expected base commit and repeat authorization checks for writes.
- Use dedicated worktrees/workspaces for concurrent work, explicit write ownership, and separately reconciled artifacts.
- Record source revision, toolchain, verification commands and observed return status. A test written but not executed is not verified.
- Create a reviewable pull request with precise summary, changed paths, risks, rollback considerations and verification evidence.
- Resolve review comments and conflicts before merge. Merge only when user authority and repository protection permit it. Check the resulting main commit.
- A merged source change is not a deployment. Signed artifacts, NixOS evaluation, staged activation, health and rollback are controlled by an independent release principal.

## GitHub API safety

Prefer a connected GitHub tool that enforces repository permissions. Follow least privilege; do not copy personal access tokens into source, tool arguments or workspace artifacts. Never infer broad administrator capabilities from read-only GitHub access. When an operation times out or is ambiguous, retrieve authoritative state before attempting a retry. Do not force-push or delete branches carrying unmerged work merely to clean up.

## Native GitHub features

- **Issues:** Use for actionable defects, requirements and acceptance criteria. Link work to affected revisions, PRs and upstream dependencies; avoid redundant tickets for completed changes.
- **Projects V2:** Use the actual board, views, fields, iterations and linked issues or PRs when a board is available. Homelab's project ID remains authoritative for compute jobs, project knowledge, repositories and artifacts; a GitHub Project is a planning view, not an alternate compute database.
- **Branches:** Keep the base commit and expected head pinned. Use one task-specific branch/worktree per independently owned change; never silently force-push, replace protected history or delete unmerged work.
- **Pull requests:** Include scope, test evidence, exact base/head, rollback considerations, linked issues and requested review. Resolve failures and checks before merge; merging source is not deploying a NixOS generation.
- **Packages / GHCR:** Prefer provenance-verifiable, versioned, digest-pinned artifacts with scoped publish credentials and explicit licensing. Publishing to GitHub Packages never by itself authorizes runtime image admission, project scheduling or production rollout.
- **Wiki:** If enabled, publish discoverable user guidance there while keeping normative technical requirements, security policies and reproducible deployment procedures in version-controlled repository files. Link the two rather than allowing conflicting documentation.
- **Actions:** Inspect workflows, check runs and remaining quota before triggering CI. Use scoped path filters, dependency caching, bounded jobs and cancellation of superseded runs without reducing required acceptance coverage.

## Local CI when hosted Actions is unavailable

1. Determine whether the blocker is actual GitHub Actions minutes/quota, runner unavailability, a failing test or a pending check. Keep the correct status visible.
2. Select the workflow and its canonical commands from the pinned commit. Use a clean, detached checkout. For Homelab, use `scripts/verify-offline.sh` where its dependencies are available, or reproduce its selected checks including Go test/race/vet/build, Rust tests/clippy, React checks, Helm policy tests and NixOS flake evaluations as applicable. Do not claim equivalence if required jobs were omitted.
3. Use an approved isolated machine or Compute project worker with bounded CPU, memory, storage, runtime and immutable dependencies; leave live Kubernetes controllers, hardware leases, EDA jobs and other owners' work unchanged.
4. Record exact source SHA, toolchain versions, commands, exit codes, test output, artifact checksums and any missing tests. Attach evidence in authorized Projects artifacts or a GitHub PR; never publish secrets or private owner assertions.
5. Distinguish `Actions-pass`, `local-pass`, `blocked-by-quota`, `not-run`, `skipped` and `failed`. Locally successful tests do not satisfy GitHub's required check contexts automatically; obtain the authorized review or rerun the required checks when hosted capacity returns.
6. Deploy only through the independent owner-approved, exact-commit release mechanism, regardless of where tests ran.
