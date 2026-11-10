# First-party interactive view contracts

The browser console is implemented with Rust/Dioxus and uses an
owner-authenticated, same-origin HTTP session. Embedded MCP Apps are separate
protocol clients of their MCP host. Their resource URIs, tool calls, and host
lifecycle must remain stable until an equivalent Rust/WASM view is available.

| Owner | Resource or route | Host operations | User interaction |
| --- | --- | --- | --- |
| Compute | `GET /dashboard` | Direct authenticated Compute HTTP requests | Worker capacity, project workspace creation, image pools, node and device inspection |
| Learning | `ui://adaptive-teaching/learning-workspace.html` | `ui/initialize`, `tools/call`, tool results and notifications | Knowledge map, assessment problems, teacher conversation, reviews, source attachments and bounded file uploads |
| Health | `ui://personal-health-mcp/workout-editor.html` | `ui/initialize`, `tools/call` | Versioned workout-plan list and save, session and exercise editing, revision conflict reporting |
| Health | `ui://personal-health-mcp/workout-companion.html` | `ui/initialize`, `tools/call` | Session selection, exercise set logging, recorded-workout completion |
| Health | `ui://personal-health-mcp/groceries.html` | `ui/initialize`, `tools/call` | Grocery list reconciliation, meal-plan ingredients, version-fenced grocery saving |
| Health | `ui://personal-health-mcp/dashboard.html` | `ui/initialize`, `tools/call` | Dated metric series, nutrient comparisons, owner-entered target updates |
| Health | `ui://personal-health-mcp/cognitive-test.html` | `ui/initialize`, `tools/call` | Five randomized visual response trials, early-response rejection, median and individual observation saving |

## Protocol and authority boundaries

The five Health MCP Apps and the Learning MCP App request
`ui/initialize` with the MCP Apps protocol version `2026-01-26`,
then notify `ui/notifications/initialized`. Tool invocations are
`tools/call` JSON-RPC requests forwarded by the host. Their embedded
views never receive credentials held by a backend service.

An independent Rust/Dioxus page is not interchangeable with an MCP App
resource. The standalone console uses an owner session, an exact same-origin
HTTP path and a CSRF token for writes. An embedded app must instead
authenticate through the MCP host and implement its own initialization,
message-correlation, bounded-error, teardown, upload and render contracts.
A native console implementation is not grounds to remove the embedded
resource until that host behavior is preserved.

The native visual reaction-time page writes through
`POST /v1/health/reaction-test`. The Go transport checks the five
samples, calculates the median and mean, marks the observation as
`estimated`, and stores it through the existing Health service. The
Rust Health metrics page uses three exact owner-session tool routes for
series, daily nutrients and goal setting. Both pages preserve the
Health service's record ownership and operate without a public MCP or
API listener.

## Migration constraints

An embedded replacement must retain keyboard access, meaningful labels
and focus handling, feedback for long-running operations, read-only
rendering of untrusted text and bounded uploaded data. It must handle
host teardown, request timeouts, unavailable services and denied writes.
Only verified CSP resource origins may load executable assets; iframe
messages require exact peer validation and correlation. The host's
security context must never be copied into browser storage.

The Compute dashboard uses an explicit bearer credential and is not an
MCP App. A Rust console replacement must use owner-session routes or
equivalent bounded service authority without exposing the controller
credential in the DOM. Learning attachment uploads require explicit
size and type validation at the Go backend, in addition to client limits.

Source-only Rust components require WebAssembly compilation, asset
packaging, protocol conformance and browser acceptance before the
corresponding legacy resource can be removed.
