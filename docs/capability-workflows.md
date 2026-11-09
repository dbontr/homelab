# Cross-capability workflows

Homelab maintains a single project identity across separately owned records. The
platform obtains project context, repository membership, and shared-library links
from the Compute controller; it does not create a second project database. The
`homelab:project:<id>` resource type is resolved against the owning service.
Personal tasks can link that reference through their existing `links` field;
the project overview then includes only explicitly associated tasks and goals.
Engineering run summaries are joined by the exact project identifier.

Read-through resource types are `project`, `engineering_run`,
`learning_session`, `health_plan`, `task`, `goal`, `recipe`,
`meal`, and `book`. Learning and health records remain stored in
their respective services. If an owner is offline or its advertised read tool
is unavailable, resolution fails closed rather than inventing a record.

## Approved durable workflows

`POST /v1/workflows` and MCP `workflow_submit` accept a project ID,
a bounded objective, explicit `personal_read` consent, and 1–12 named stages.
Each stage has a stable ID, dependency IDs, and one approved read action:

- `project_context` — authoritative project knowledge context
- `project_tasks` — explicitly linked tasks and their goals
- `research_search` — scoped search of project-linked research passages;
  includes source locators from the controller
- `engineering_runs` — matching engineering run summaries
- `learning_summary`, `health_goals`, `recipe_search`,
  `media_requests` — opt-in personal application reads

The last four operations require `personal_read: true`. The program cannot
name arbitrary MCP tools, issue shell commands, call privileged executor
operations, make purchases, submit engineering changes, or publish Git
revisions. Project existence is checked against the Compute controller
before workflow creation; stage authorization is revalidated during dispatch.

A program might look like:

```json
{
  "project": "research",
  "objective": "Find evidence and related engineering work on Fourier methods",
  "steps": [
    {"id": "context", "action": "project_context"},
    {"id": "sources", "action": "research_search", "query": "Fourier", "limit": 8, "depends_on": ["context"]},
    {"id": "engineering", "action": "engineering_runs", "depends_on": ["sources"]}
  ]
}
```

The scheduler advances dependency-ready steps without further input. It shares
the existing Go lease-token, generation-fencing, optimistic-revision,
cross-process-locking, and atomic-state mechanisms. Read operations can be
repeated safely after an expired lease; uncertain mutating operations have no
route through this scheduler. Each run has a 30-minute absolute deadline and
each read stage has at most two attempts. Results are bounded and persisted
privately so a process restart does not erase confirmed evidence.

Read status via `GET /v1/workflows` or `GET /v1/workflows/{id}`, or
MCP `workflow_list` / `workflow_get`. Listing omits recorded evidence
bodies. Detail reads are authenticated. Cancel through an exact expected
revision (`POST /v1/workflows/{id}/cancel` or `workflow_cancel`).
The browser session may read summaries but cannot submit or cancel without
the independent owner-write authorization path.

The state directory is `workflows` under the platform's private state
directory. As with engineering runs, archive and retention management must
be implemented before the store reaches its 1,000-record limit.

## Scope and acceptance

This implementation provides autonomous *execution* of an explicitly
authorized dependency plan. It does not yet give a language model unattended
authority to invent new capability operations or change personal records.
It does not supersede project-owned learning, health, recipe, or media logic.

Prior to service activation, verify every modified Go module, Rust/WASM build,
Nix evaluation, controller-side project grants, restart recovery, negative
authorization tests, and service endpoints. A successful read-only workflow
is not evidence that the entire product is fully integrated or deployment-ready.
