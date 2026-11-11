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
The five Health views reuse Dioxus components and call `tools/call`
through the host rather than exposing owner-session CSRF credentials.
The Learning view uses an exact independent Learning tool allowlist. It
cannot call credential, grading, execution, or answer-key operations.
The bridge handles host-provided tool results, bounded requests, timeouts,
explicit resource teardown and parent-window source validation.

`ui/console/build-mcp-apps.sh UNUSED_ABSOLUTE_OUTPUT_DIRECTORY`
builds the Rust/WebAssembly distribution and generates six resource
entrypoints, five Health views and one Learning workspace, with fixed
`homelab-mcp-view` markers. The source-owned Go services verify every
generated asset against
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

The source library can request a deterministic preview of at most 80
reviewed curriculum steps through an owner-session route. Each preview
identifies source coverage, blocked extraction or review work, exact
source quotations, prerequisite links and the evidence digest. Import
requires the owner to confirm the ready preview and submits that exact
digest and selection limit to the Learning store. The store recomputes
eligibility within its transaction and refuses stale or unreviewed
evidence. Import creates concepts and prerequisites only; it does not
create assessment records or increase mastery.

## Learning MCP App release

The Rust Learning MCP App is opt-in through
`services.adaptiveTeachingMcp.model` for private model proposals and
`services.adaptiveTeachingMcp.appBundle.directory` with
`services.adaptiveTeachingMcp.appBundle.origin` for the verified UI bundle.
The Learning service retains its original embedded HTML resource while
the app bundle is disabled. With a verified release, the exact same
`ui://adaptive-teaching/learning-workspace.html` URI returns the
Learning Dioxus entrypoint and resource-level CSP metadata. Its public
asset route serves only manifest-verified JavaScript, WebAssembly, CSS
and static resources. The private MCP listener and its bearer token remain
separate.

The embedded view supports host-provided graph, reasoning, question,
session, problem, source, review and practice results. The learner can
submit an ungraded response and voluntary confidence estimate. The
session can request explicitly consented, provisional model feedback on
one independent reasoning step. Reviewed sources can be previewed and
imported against the store's exact evidence digest without awarding
mastery. Files attached to an existing session, problem or source travel
only through approved MCP host tools, with a five-MiB browser limit and
operation-specific bounded JSON-RPC envelopes. The Learning store verifies
MIME signatures and SHA-256 integrity before a download is prepared as
a local browser Blob. The host must permit sandbox downloads before
download links are operational.

Model calls are disabled unless a private provider endpoint and model
identifier are configured. The owner must opt into disclosing source
passages or learner responses for each model proposal. These proposals
cannot change grades or mastery by themselves: the Learning store remains
the sole authority for review, grading and learner progress.

The Compute Dioxus console has exact owner-session routes for resource
planning, workspace creation and teardown, immutable image and software
catalog registration, and revision-qualified program/catalog review.
Origin, CSRF, project/device admission and independent privileged
authorization remain mandatory; no controller bearer credential is
copied into the browser.

WebAssembly compilation, actual MCP host permission handling, device
custody and browser accessibility still require acceptance before the
legacy embedded Learning resource or Compute dashboard is retired.
