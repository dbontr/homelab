# Research evidence and compute planning

## Automatic local source ingestion

An owner may enable `research_text_policy_set` for a specific shared library with an exact policy revision. The controller then indexes immutable UTF-8 `.txt`, `.md` and `.json` objects up to 96 KiB into bounded source-linked passages. The index worker verifies each object's SHA-256 and rechecks permission inside the passage transaction. Invalid documents retain visible failure records and do not block subsequent sources. A separate embedding policy controls whether passages may be transmitted to a model; text ingestion never enables embeddings by implication.

MCP tools: `research_text_policy_get`, `research_text_policy_set`, `research_text_index_pending`, and `research_text_failures`. Native policy routes: `GET /api/projects/{project}/research/text-policy/{library}` and owner-only `PUT /api/libraries/{library}/research/text-policy`. PDF, scanned, multimodal and repository extraction require source-specific extractors; they are not mistaken for plain text.

## Hybrid evidence retrieval

`research_hybrid_query` and `GET /api/projects/{project}/research/hybrid?q=...` combine project-linked FTS5 search with semantic retrieval when the project has active embedding consent and a configured provider. A bounded reciprocal-rank-fusion score (rank constant 60) merges duplicate source passages without inventing source claims. If semantic retrieval is unavailable, the response explicitly reports `lexical_only` rather than falsely claiming semantic results. Both pathways rely on existing project-library authorization and immutable citation locators.

## Evidence graph

The Compute controller owns project-level research claims and hypotheses. Each assertion has an immutable project identity, stated kind, revision and separately linked indexed passage evidence. Passage relations can be `supports`, `challenges` or `context`; they are not verdicts. An owner/editor may attach verified indexed evidence only from a library linked to that exact project. The source epoch, hash and current project-library authorization are rechecked when reading a claim. Replaced, tampered or unlinked passages become unavailable references rather than silently continuing as support. Mixed support and challenge produce `contested_evidence`, not an assertion that a claim is true or false.

The REST operations are `GET/POST /api/projects/{project}/research/claims`, `GET /api/projects/{project}/research/claims/{claim}`, and `POST /api/projects/{project}/research/claims/{claim}/evidence`. MCP clients use `research_claims`, `research_claim_detail`, `research_claim_create` and `research_claim_evidence_add`. Explicit research workflows can read project claims without copying or mutating the learning service's mastery graph.

## Workload placement

The controller offers `resource_plan_batch` through MCP and `POST /api/projects/{project}/resource-plan/batch` through the native API. A bounded batch (up to twelve workloads) is sorted by owner-specified priority, hardware scarcity and resource demand. Each workload independently passes the existing project, node heartbeat, image, architecture and accelerator checks. The planner then accounts for CPU, memory and conservatively exclusive accelerator kinds promised to earlier placements in the same advisory batch.

The output is **not** a hardware reservation, job submission or guarantee of timing. When a worker actually creates a workspace, independent current admission checks and hardware leases remain authoritative. The batch planner will decline some valid multi-accelerator placements rather than promise an unknown GPU/NPU device identity. The controller reports unplaced work explicitly.

## Owner-authorized actions

The platform's `workflow_submit` accepts `personal_write: true` for an exact owner-authored `task_create` stage. That stage creates a project-linked task through the personal store, which persists the task and an idempotency receipt in one atomic record update. Repeated execution with the same stage and payload returns the existing task. Mismatched reuse of the receipt fails. A finite receipt retention limit prevents unbounded state growth.

The model planning and continuation contracts retain their separate **read-only** allowlist: neither a prompt, retrieved passage nor a prior workflow's personal-read permission can authorize a write. More sensitive actions such as publishing code, changing health records or administering infrastructure must be implemented in their actual owner services with independent authorization, separate confirmation when required, and reliable idempotency before being added to this execution surface.
