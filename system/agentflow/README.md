# Engineering workflow engine

The `agentflow` package implements bounded project-scoped engineering runs for the Homelab platform. Its operations are owned by an independent `homelab-engineeringd` process. The public API and MCP gateway communicate with that process over a restricted Unix socket and do not receive its compute or model credentials.

A run validates an acyclic dependency graph and executes an approved series of steps: workspace provisioning, exact-revision repository checkout, bounded source inspection, scoped authorship, source application, command verification, artifact export and draft-only publication. The compute controller and worker are internal execution components. Workspace resource limits, repository membership and image admission remain enforced by that controller.

Workflow state uses private atomic records and an advisory cross-process file lock. Claims have expiring leases, monotonically increasing generations and stored token digests. Only the active token and generation may confirm an operation. A timeout does not establish whether an external effect occurred. Such a step is not retried automatically without independent reconciliation; each compiled external step therefore has a single execution attempt. Owner cancellation fences pending and running work.

An operator may configure a bounded chat-completions model to generate full source file contents for an explicit allowlist. The model never receives credentials and cannot select a new target, execute a shell command, change the repository scope or publish directly. Verification commands execute only inside the isolated compute workspace. Successful checks permit the production-independent source publisher to create a **draft** pull request, not merge or deploy it.

For service configuration, API routes, runtime assumptions and acceptance criteria, see [Engineering execution](../../docs/engineering.md).
