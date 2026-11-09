# Cross-capability workflows

## Owner-configured recurring automation

A persistent scheduler runs read-only capability workflows at owner-configured intervals of 60–10,080 minutes. Schedules are explicit programs with an immutable project, stage dependency graph and personal-read flag. Model replanning, arbitrary commands and **all personal/privileged writes are forbidden in automatic schedules**; write-capable workflows remain separately submitted by the owner. State lives next to the durable workflow engine in a private, revision-fenced automation record, not a second project database. Each due occurrence produces a deterministic workflow ID. A process restart or ambiguous persist operation cannot duplicate the occurrence, and pause/resume is guarded by the expected automation revision. Missed periods are not flooded into a backlog; the next interval begins after a confirmed enqueue. A disconnected project backend leaves the due record outstanding rather than claiming a completed run.

The API offers `GET/POST /v1/automations`, `GET /v1/automations/{id}`, and `POST /v1/automations/{id}/enabled`. MCP offers `automation_create`, `automation_list`, `automation_get` and `automation_set_enabled`. The owner can inspect the last workflow ID and its confirmed result. The scheduler produces durable, inspectable results. A schedule may opt into `notify_on_change`. The first successful result establishes an observation baseline. After subsequent successful scheduled runs, the owner-private automation store compares canonical stage results; a difference creates a durable alert containing only the workflow reference, project and observation time. Failed and incomplete runs do not trigger misleading changes. Each alert can be acknowledged using the schedule's current revision. The scheduler reconciles an outstanding result before enqueuing the next run for change monitoring, avoiding skipped observations across restarts. Up to 32 alerts per schedule are retained.

Read change alerts through `GET /v1/automations/alerts` or MCP `automation_alerts`, then acknowledge through `POST /v1/automations/{id}/alerts/{run}/ack` or `automation_alert_acknowledge`. The Rust console exposes both actions. Alerts contain no observed payload, and full results still require workflow detail authorization. This is a first-party notification inbox, not an external push or messaging provider; no outbound personal-data delivery is implied.


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
Each stage has a stable ID, dependency IDs, and one explicitly approved action. Read-only actions include:

- `project_context` — authoritative project knowledge context
- `project_tasks` — explicitly linked tasks and their goals
- `research_search` and `research_semantic_query` — keyword or consented model-generated semantic search of project-linked research passages;
  includes source locators from the controller
- `research_claims` and `research_claim_detail` — source-linked project hypotheses, conflicting evidence and explicit unavailable provenance
- `engineering_runs` — matching engineering run summaries
- `learning_summary`, `learning_sessions`, `learning_reviews`,
  `health_goals`, `health_records`, `health_training_sets`,
  `health_food_proposals`, `recipe_search`, `recipe_meals`,
  `recipe_groceries`, `recipe_nutrition`, `task_review`,
  and `media_requests` — opt-in personal application reads
- `study_pack_preview` — compose an unsaved, cited study pack from successful prerequisite `research_search` results

Every personal application read, including `project_tasks`, requires `personal_read: true`.

Explicitly owner-submitted programs may also include `task_create`, `goal_create`, and `task_complete` when `personal_write: true` is present. Task creation links its record to the exact project; completion requires an existing task linked to that project and a matching `expected_revision`. The authoritative personal store writes each outcome and its stage-specific idempotency receipt atomically, including any recurring successor task. Repeated work with the same receipt returns the confirmed record; unrelated, changed or stale operations are rejected. Goals are personal records and are not fabricated as completed research. Neither model-proposed initial plans nor model-authored continuations can add write actions.
`project_context`, `research_search`, and `engineering_runs` remain project-scoped and read-only. The program cannot
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

For workflows submitted with `auto_replan: true`, the scheduler can ask the configured model for up to two bounded continuation rounds after all preceding stages succeed. Every additional stage must depend on confirmed evidence, pass the original action and privacy policy, and fit the 12-stage total limit. Revision-fenced persistence prevents stale model decisions from changing a cancelled or modified workflow. Model failures are recorded as `planning_failed` rather than presented as successful completion. The `share_project_evidence` opt-in enables short source-linked research excerpts in model context; personal task, health, learning, recipe, and media result bodies are never forwarded.

The scheduler advances dependency-ready steps without further input. It shares
the existing Go lease-token, generation-fencing, optimistic-revision,
cross-process-locking, and atomic-state mechanisms. Read operations can be
repeated safely after an expired lease; approved personal record mutations use exactly scoped, atomically persisted
idempotency receipts. Privileged executor operations have no route through
this scheduler. Each run has a 30-minute absolute deadline and
each read stage has at most two attempts. Results are bounded and persisted
privately so a process restart does not erase confirmed evidence.

Read status via `GET /v1/workflows` or `GET /v1/workflows/{id}`, or
MCP `workflow_list` / `workflow_get`. Listing omits recorded evidence
bodies. Detail reads are authenticated. Cancel through an exact expected
revision (`POST /v1/workflows/{id}/cancel` or `workflow_cancel`).
The browser session may read summaries but cannot submit or cancel without
the independent owner-write authorization path. A persisted `personal_write`
flag is confined to its original program; it does not authorize any other
personal, external account or privileged operation.

The state directory is `workflows` under the platform's private state
directory. A private `archive` subdirectory holds terminal capability
workflow records after a 24-hour settling period. The scheduler checks
for eligible records every five minutes and moves at most 100 per pass.
Running steps, unresolved model continuations and engineering runs remain
in active storage. A cross-process lock and directory sync protect archival,
and archived IDs cannot be reused for new workflows or automation occurrences.

`GET /v1/workflows` and MCP `workflow_list` expose active workflow summaries.
`GET /v1/workflows?archived=true` and `workflow_archive_list` expose up to
100 archived workflow summaries, newest first. Both listings omit stage
result bodies. `GET /v1/workflows/{id}` and `workflow_get` can retrieve
the full authorized detail of an active or archived capability workflow.
Archived records cannot be claimed, cancelled, or otherwise revised.
Archival frees entries from the active 1,000-record limit while retaining
historical evidence; there is no automatic deletion of archived records.

## Evidence-linked personal delegations

A personal task may link an engineering run using `homelab:engineering_run:<run-id>` in its `links` list (supported by the native Task API and MCP `task_add`). The derived `/v1/tasks/delegations` view and `task_delegations` tool read run status from the independent engineering service. They report completion candidates, failed or cancelled runs, unresolved references, and mismatched project links without mutating task status or fabricating successful work. The Rust weekly planner displays these observations separately from ordinary deadlines and estimates. The view remains usable when Engineering is offline, but reports its evidence as unavailable.

## Planning and semantic retrieval

The model proposal endpoint `POST /v1/workflows/plan` and MCP `workflow_plan` accept a project, objective, and explicit personal-read scope. An independently configured HTTPS or loopback model proposes up to 12 read stages. The platform validates identity, actions, dependencies, limits, and privacy scopes without executing the plan. The owner must explicitly submit the plan through the existing workflow API. The model never receives administrator credentials or arbitrary project content.

Source deployment accepts `services.homelabPlatform.capabilityModel.{url,name,tokenFile}` for a legacy single-model planner. For purpose-specific routing, use `services.homelabPlatform.capabilityModel.routes`, with up to three explicitly declared roles: `planning` (initial read-only stage proposal), `continuation` (bounded continuation using status-only observations), and `evidence` (continuation when the owner has explicitly enabled project research excerpt sharing). The routes use independently selected fixed chat-completion endpoints and model names. The optional `localOnly` property rejects all non-loopback addresses. No fallback to a different provider or role is permitted when a required route is absent or unavailable. A workflow requesting automatic continuation is rejected at submission unless its router contains a continuation role; workflows with opted-in research evidence also require a distinct evidence role. A configured routes list and legacy single-model fields are mutually exclusive. NixOS renders only nonsecret route metadata into its store and loads each configured token through systemd `LoadCredential`; neither a token nor a provider authentication cookie appears in Git or model prompts.

Example for a local, privacy-preserving setup:

```nix
services.homelabPlatform.capabilityModel.routes = [
  { id = "plan"; purpose = "planning";
    url = "http://127.0.0.1:8001/v1/chat/completions"; model = "planner"; localOnly = true; }
  { id = "reason"; purpose = "continuation";
    url = "http://127.0.0.1:8001/v1/chat/completions"; model = "reasoning"; localOnly = true; }
  { id = "research"; purpose = "evidence";
    url = "http://127.0.0.1:8001/v1/chat/completions"; model = "reasoning"; localOnly = true; }
];
```

The routes do not instantiate an inference service themselves. A separately administered model endpoint is required for each configured purpose. The independent read-only stage and personal-data validators run after every model proposal and every continuation.

 Compute Kubernetes deployment accepts `services.workspaceMcp.embedding.{url,model,tokenSecretName}`; the Helm chart copies a preprovisioned token Secret into a private memory volume before starting the unprivileged controller. These options default to disabled. A configured model endpoint does not grant permission to share research content: library policy remains independently required.

The Compute controller can use a configured `/v1/embeddings` provider for automatic indexing. The owner must explicitly enable each shared library with `research_embedding_policy_set` before any passage text is transmitted. Policies are revision-guarded and can be revoked even when the provider is offline. The controller indexes a bounded batch of new passages periodically, validates response dimensions and model identity, checks immutable passage hashes, and fences consent and source revisions at the database write. The native `/api/projects/{project}/research/semantic?q=...` endpoint and MCP `research_semantic_query` embed the caller-provided query text, then search project-visible passages. Provider credentials are loaded only from a private file; no embedding result grants additional library access.

Research indexes may attach immutable dense vectors to indexed passages through the root-authorized `research_embedding_put` tool. The same encoder model must generate both document and query vectors. `research_semantic_search` compares query vectors only with currently active passages in project-linked libraries and returns source citations with cosine similarity. Search is bounded to 2,000 candidate embeddings. The system does not infer embeddings automatically, and stale or mismatched source epochs are excluded.

The engineering agent can request read-only `go_semantics` analysis, which performs Go AST parsing and local type checking. An analysis involving unresolved repository imports is marked partial. This does not replace integration builds, compiler checks for all module dependencies, or the owner-approved test suite.

## Scope and acceptance

This implementation provides autonomous *execution* of an explicitly
authorized dependency plan. It does not yet give a language model unattended
authority to invent new capability operations or change personal records.
It does not supersede project-owned learning, health, recipe, or media logic.

The root `scripts/verify-offline.sh --all` checks Go tests, race tests, vet and all application commands, Rust formatting/tests/WASM compilation, Nix flake checks, enrolled NixOS derivation evaluation, and core Nix package builds. The script performs no activation or release promotion. Each source module requires its toolchain and dependencies to be cached for offline execution.

Prior to service activation, verify every modified Go module, Rust/WASM build,
Nix evaluation, controller-side project grants, restart recovery, negative
authorization tests, and service endpoints. A successful read-only workflow
is not evidence that the entire product is fully integrated or deployment-ready.
