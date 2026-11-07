# Interfaces

The Go platform exposes an authenticated loopback HTTP server. The native JSON API and the primary MCP endpoint invoke the same task, goal, and rule operations.

## Native API

- GET /healthz: readiness, without sensitive details
- GET /v1/tasks and POST /v1/tasks: task retrieval and creation
- POST /v1/tasks/{id}/complete: idempotent completion
- GET /v1/goals and POST /v1/goals: long-term objectives
- GET /v1/rules and PUT /v1/rules/{key}: personal rules
- GET /v1/recommendations: deterministic selection by time, energy, context, and quick-win mode
- GET /v1/reports/weekly: observed task completions and progress against the minimum

All non-health endpoints require a bearer token loaded from a runtime credential file. The gateway listens on loopback and requires an authenticated ingress for remote access.

## MCP

The primary endpoint is POST /mcp. Native tools use task, goal, rule, and report operations. The gateway can expose configured Workspace, Learning, and Health tools through prefixed internal names. The namespacing prevents collisions; clients present one assistant experience.

An upstream failure does not erase local operations. The gateway does not silently turn a failed mutation into a successful one.

## Personal operating rules

The baseline minimum is three completed tasks per day. Any completed task may count toward that threshold; quick errands are intentionally eligible. Goal progress, independent learning, and workload significance are reported separately. Natural-language rules may be stored as preferences, but only explicitly implemented structured constraints are mechanically enforced.

Reports only measure observed completion records. The absence of an event is not evidence that an activity did not occur.
