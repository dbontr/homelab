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
