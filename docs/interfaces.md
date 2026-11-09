# Interfaces

The Go platform exposes authenticated loopback HTTP endpoints. Native JSON and MCP clients invoke the same business operations.

The unified MCP discovery operation preserves the canonical local tool set when an integration is unreachable. Federated `tools/list` requests run concurrently under one four-second deadline, and returned entries retain configured backend order. An unavailable backend contributes no advertised tools; its timeout does not delay all other backends sequentially. Duplicate exported tool names are rejected after namespace normalization, so one tool identity cannot silently change owners. Tool execution still independently checks the selected backend's currently advertised operations and its authorization policy. The `system_status` observation applies the same single-deadline isolation principle to backend health checks. Each integration is available only when its tool-discovery envelope is valid; an unreachable or malformed backend is reported unavailable, without suppressing the local service status.

## Native API

- GET /healthz: process readiness without sensitive details
- GET /v1/tasks and POST /v1/tasks: list and create tasks
- POST /v1/tasks/{id}/complete: idempotently mark a task complete
- POST /v1/tasks/{id}/decompose: atomically create 2–32 actionable subtasks from `{"expected_revision":1,"steps":[{"title":"...","minutes":30,"depends_on_steps":[0]}]}`; every dependency index must refer to an earlier step. The operation updates the parent revision, inherits its goal, project links, deadline and prerequisite context, and creates all subtasks or none.
- GET /v1/goals and POST /v1/goals: list and create long-term goals
- GET /v1/rules and PUT /v1/rules/{key}: list and save personal constraints or preferences
- GET /v1/recommendations: filter by available time, energy and location; quick mode favors small tasks
- GET /v1/reports/weekly: recorded completions against the daily minimum and associated goal progress
- GET /v1/calendar: read-only event definitions from an optional iCalendar sharing link
- GET /v1/calendar/occurrences: bounded expanded events and recurrence limitations
- GET /v1/planning/week: advisory weekly allocation of tasks into unoccupied calendar time, with optional `day_minutes` (seven comma-separated daily budgets, zero or 30–480); respects dependency ordering across the plan and never mutates tasks or the calendar
- GET /v1/monitoring/summary, /series, /targets, /alerts, /logs: owner-authenticated fixed telemetry and journal views from configured local Prometheus and Loki sources
- GET /v1/devices: declared device inventory and access policy, not live device status
- GET /v1/nodes: enrolled and agentless node inventory, not live node status
- GET /v1/software: read the Workspace-backed approved image, software, and program catalogs
- GET /v1/tools: discover local and configured backend capabilities using the MCP tool catalog
- POST /v1/tools/{name}/call: invoke the same authorized operation as MCP, with a JSON body containing `arguments`; the result uses MCP tool-content semantics
- GET /v1/compute/profiles: list reusable, versioned build configurations
- GET /v1/compute/profiles/{id}?version={n}: read a saved revision, or the latest if no version is supplied
- PUT /v1/compute/profiles/{id}: save a profile with an exact `expected_version` revision guard
- POST /v1/compute/profiles/{id}/plan: request advisory Workspace resource planning without reserving resources
- GET /v1/books/search?q=...: bounded owner-initiated Open Library catalog lookup when explicitly configured
- GET/POST /v1/books/requests and PUT /v1/books/requests/{id}: private desired-book list and manual fulfillment/cancellation with revision control
- GET /v1/activity?limit=...: recorded activity derived from canonical personal and recipe data
- GET /v1/resources/{kind}/{id}: canonical record with stable resource reference and links, read from its owning store. Goals expose related tasks, tasks expose parent/child and prerequisite/dependent relations, and recipes and planned meals link in both directions. These relations are derived at read time without copying or modifying private owner records. Learning session references support the full 128-character identity limit.
- GET /v1/media/search: Seerr movie and television search, when configured
- GET /v1/media/requests and POST /v1/media/requests: list and submit requests

All endpoints other than /healthz require authorization. The platform accepts its protected API bearer token or, for an explicitly limited subset of native routes, an authenticated short-lived browser session with Origin and CSRF checks for permitted mutations. Browser sessions cannot call MCP tools or execute arbitrary compute and cluster operations. The server binds to loopback and requires separately authenticated ingress for remote clients.

## MCP

The primary endpoint is POST /mcp. It supports JSON-RPC 2.0 over application/json and, when a client explicitly accepts text/event-stream, a Streamable HTTP SSE response containing one final JSON-RPC message. MCP notifications receive HTTP 202 with no response body. Event-stream formatting does not imply incremental progress events: the tool operation finishes before the final message is emitted. JSON clients and existing browser authentication requirements retain their original behavior. Local tools correspond to the task, goal, rule, report and status functions. The node_inventory tool lists declared host roles. The compute_profile_list, compute_profile_get and compute_profile_save tools operate on the same persisted build configurations as the native API. The software_catalog tool combines the Workspace-approved image, software and program pools without duplicating their persisted state. The device_inventory tool lists declared hardware constraints without making a physical connection. The calendar_feed tool is available only when a calendar sharing link is configured. The media_search, media_requests and media_request tools are available only when the Seerr adapter is configured. The approval_status tool reports whether a specific administrator request remains pending or approved. Configured Workspace, Learning, and Health tools can be exposed with collision-free internal prefixes without creating separate user-facing assistants.

MCP tool calls are not a privileged deployment pipeline. Upstream mutations use explicitly configured credentials and report failures rather than fabricating success.

## Personal operating rules

Tasks may declare a parent and prerequisites through `parent_id` and `depends_on`. The `task_decompose` MCP operation shares the native decomposition transaction. A decomposed parent is a non-actionable container: the agenda, suggestions and weekly planner select its subtasks, and parent completion is rejected while any recorded subtask remains open. Existing ordinary parent relationships are unaffected. Recommendations exclude tasks with incomplete recorded prerequisites. Both interfaces use the same task data and rules. The baseline minimum is three completed tasks per day. Any completed task can count; quick errands are intentionally eligible. Goal progress and the significance of a particular accomplishment remain separate measures. Natural-language rules are stored as preferences or constraints, but only explicitly implemented structured constraints are mechanically enforced.

Reports measure recorded completions. A missing record does not establish inactivity.

## Calendar limitations

The iCalendar sharing link is read-only and stored outside Git. The implementation unfolds event definitions and expands a bounded subset of daily, weekly, monthly and yearly recurrence rules, including exclusions, additional dates and overridden instances. Each exception keeps its own iCalendar TZID, and recurrence identity without an explicit zone is interpreted in the parent event's zone. Expanded events are sorted chronologically by UTC instant rather than local timestamp text. The recurrence reader accepts monthly ordinal weekdays, last-weekday positional rules, and all-day events without DTEND, preserving local calendar-day durations through daylight saving changes. Positional filters outside monthly recurrence, unsupported timezone identifiers and other unimplemented recurrence syntax remain explicit limitations rather than assumed free time. Calendar availability is advisory: it does not certify that a time is free of unrecorded commitments. The weekly planner uses task prerequisites, recorded durations and discovered calendar reservations without writing back to either system.
