# Engineering workflow orchestration

The `agentflow` package persists bounded, dependency-aware engineering work without giving the workflow scheduler privileged credentials. Each workflow belongs to one project; each step has a named operation kind, a JSON input contract, explicit prerequisite steps and a finite retry budget.

A dispatcher supplies the actual isolated execution and must enforce its own operation allowlist, project authorization, resource limits and output filtering. The coordinator itself cannot execute arbitrary code on the host or access the production cluster. Stable `workflow/step` idempotency keys allow an executor to deduplicate a repeated operation after a worker crash or lease timeout. Do not use a dispatcher that repeats irreversible effects without idempotency.

State is stored in private, atomically replaced JSON records, serialized by a process-wide file lock. Each claim has an expiring lease, monotonically increasing generation and unpredictable secret token. The persisted token digest fences stale or forged worker completions. A cancellation invalidates all pending and active steps. A crash does not certify that an external side effect failed; reconciliation with the executor is required before retrying potentially irreversible operations.

The package provides an execution engine and durable state primitives, not a full autonomous coding agent. Integration with authenticated Workspace operations, approval-gated publishing, evaluator feedback, prompt/tool policy and the Rust console remains separate development work. It is not wired to NixOS service activation or privileged executor permissions.
