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

## Rust MCP Apps source implementation

The console's Cargo feature `embedded-app` changes its Rust root
component from the owner-session console to an MCP Apps host client. The
`ui/console/src/mcp_app.rs` transport checks the exact parent window,
uses JSON-RPC request IDs, bounds each payload and outstanding requests,
times out unacknowledged calls, and performs the
`ui/initialize` / `ui/notifications/initialized` handshake.
The five Health views reuse the same Dioxus components, but call
`tools/call` through the host rather than exposing the
owner-session CSRF credential inside the iframe.

`ui/console/build-mcp-apps.sh UNUSED_ABSOLUTE_OUTPUT_DIRECTORY`
builds the Rust/WebAssembly distribution and generates five resource
entrypoints with fixed `homelab-mcp-view` markers. The
source-owned Go Health service verifies every generated asset against
`SHA256SUMS`, refuses symlinks and undeclared bytes, and
offers public static assets only from the verified bundle.

To opt in, configure
`services.personalHealthMcp.appBundle.directory` and
`services.personalHealthMcp.appBundle.origin` together.
The latter is the exact HTTPS origin where the service's public static
asset routes are reachable. The Go Health service then advertises this
origin as permitted `resourceDomains` and `connectDomains` in
each UI tool's MCP metadata and serves the resource URI using the
corresponding WASM entrypoint. Its Go service and underlying Health
database remain authoritative.

When both options are unset (the default), the original embedded HTML
resources are returned unchanged. The asset origin must be routed
externally to the public static file handlers without making the private
MCP endpoint or user records public. The asset origin is not an
authentication mechanism; the host continues to mediate Health tool
calls. Compilation, bundle compatibility, CSP enforcement, asset URL
resolution, browser accessibility, data preservation and fallback behavior
still require acceptance before enabling this configuration.


## Learning session and original-file custody

The standalone Learning library provides Dioxus inspection for concept
graphs, reasoning-chain provenance, evidence, problem statements, source
records, practice history and delayed retrieval obligations. The Go
owner-session read routes allow only specific Learning inspection tools.
Grading, answer-key disclosure and general MCP tool execution are excluded.

The Learning console presents the next action and diagnostic recommendation
for a selected session. The owner can build a prerequisite plan or advance
a session only with an explicit expected revision. Learning independently
checks phase transitions, pending grading, prerequisite coverage, and
completion evidence. These operations cannot award mastery.

An owner can preserve original work against a problem, learning session,
or source using the same-origin binary upload route
`POST /v1/learning/attachments`. The source-owned Go transport checks the
owner-session cookie, Origin, CSRF, permitted MIME type, owner identity and
a five-MiB file limit. The source receives its filename as a percent-encoded
header and its bytes as the HTTP body. A separate, narrowly scoped
Learning call forwards the file to the Learning store; the general MCP
gateway retains its one-MiB tool-argument ceiling.

`GET /v1/learning/attachments/{kind}/{id}` returns original-file metadata.
`GET /v1/learning/attachment-originals/{id}` retrieves a stored original
through the private Learning backend, verifies the reported SHA-256 and
returns it as an attachment download with `nosniff` and no-store caching.
The browser transfer limit is five MiB; larger originals retained through
Learning's other interfaces cannot be downloaded through this route.
Uploads do not imply interpretation, a correct answer, or mastery.

The embedded Learning App remains responsible for model-mediated
assessment, source extraction, diagnostics and the full adaptive-teaching
workflow until the native interfaces achieve functional parity.
