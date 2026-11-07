# Interfaces

The Go platform exposes authenticated loopback HTTP endpoints. Native JSON and MCP clients invoke the same business operations.

## Native API

- GET /healthz: process readiness without sensitive details
- GET /v1/tasks and POST /v1/tasks: list and create tasks
- POST /v1/tasks/{id}/complete: idempotently mark a task complete
- GET /v1/goals and POST /v1/goals: list and create long-term goals
- GET /v1/rules and PUT /v1/rules/{key}: list and save personal constraints or preferences
- GET /v1/recommendations: filter by available time, energy and location; quick mode favors small tasks
- GET /v1/reports/weekly: recorded completions against the daily minimum and associated goal progress
- GET /v1/calendar: read-only event definitions from an optional iCalendar sharing link
- GET /v1/media/search: Seerr movie and television search, when configured
- GET /v1/media/requests and POST /v1/media/requests: list and submit requests

All endpoints other than /healthz require a bearer token supplied through a protected runtime credential file. The server binds to loopback and requires separately authenticated ingress for remote clients.

## MCP

The primary endpoint is POST /mcp. Local tools correspond to the task, goal, rule, report and status functions. The calendar_feed tool is available only when a calendar sharing link is configured. The media_search, media_requests and media_request tools are available only when the Seerr adapter is configured. The approval_status tool reports whether a specific administrator request remains pending or approved. Configured Workspace, Learning, and Health tools can be exposed with collision-free internal prefixes without creating separate user-facing assistants.

MCP tool calls are not a privileged deployment pipeline. Upstream mutations use explicitly configured credentials and report failures rather than fabricating success.

## Personal operating rules

The baseline minimum is three completed tasks per day. Any completed task can count; quick errands are intentionally eligible. Goal progress and the significance of a particular accomplishment remain separate measures. Natural-language rules are stored as preferences or constraints, but only explicitly implemented structured constraints are mechanically enforced.

Reports measure recorded completions. A missing record does not establish inactivity.

## Calendar limitations

The iCalendar sharing link is read-only and stored outside Git. The implementation unfolds event definitions, extracts dates, titles, locations, time-zone labels and recurrence rules. It does not expand recurring events, apply recurrence exceptions, or certify that an apparent free period has no commitment. Calendar-aware availability recommendations require full recurrence processing and live acceptance tests.
