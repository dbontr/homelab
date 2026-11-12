# Homelab Console

The console is a Rust/Dioxus client compiled to WebAssembly for the Go personal computing platform. The browser interface uses standard HTML and CSS with shadcn/ui neutral design tokens. First-party application logic is written in Rust, without TypeScript, JavaScript, JSX, TSX, or React source. The Dioxus build may emit generated JavaScript required to initialize WebAssembly; this is not an application development language.

## Development

From `ui/console`:

```sh
dx serve --platform web
```

Requires a Rust toolchain with the `wasm32-unknown-unknown` target, Dioxus CLI 0.7, and cached Cargo dependencies. Use a local reverse proxy to serve the compiled console and Go API under one HTTPS origin. Dioxus is a web application; the API must be reachable under the same origin.

Do not expose the gateway's MCP bearer secret to the browser. Cloudflare Access authenticates the owner at `dbontr.me`; Go verifies the signed Access assertion, including issuer, application audience, lifetime, and exact owner identity, on each native browser API request. The read-only `GET /ui/session` returns authentication status and an assertion-bound CSRF token. Permitted personal-data mutations also require the exact allowed Origin and `X-CSRF-Token`; signed browser identity cannot change cluster configuration or invoke MCP tools. Signing out uses `/cdn-cgi/access/logout`.

Configure `HOMELAB_CONSOLE_ACCESS_ISSUER`, `HOMELAB_CONSOLE_ACCESS_AUDIENCE`, `HOMELAB_CONSOLE_ACCESS_OWNER_FILE` and `HOMELAB_CONSOLE_ACCESS_JWKS_FILE` together. NixOS supplies the owner identity and pinned signing keys through protected systemd credentials; the audience must match the Cloudflare Access application at `dbontr.me`. Configure `HOMELAB_ALLOWED_ORIGINS` for the exact HTTPS origin. Missing or invalid Access verification fails closed. Infrastructure administration remains separately protected by the Rust authority's password, TOTP and operation policy.

## Views

Overview, Projects, Compute, Software Library, Tasks, Learning, Health, Recipes, Media and Administration consume live API responses. The Tasks view supports quick capture, recurrence, scheduled timeboxes, a date-aware agenda and owner-scoped edits. Projects can inspect isolated Workspace jobs and environments without triggering execution. Learning supports creating and inspecting sessions and submitting ungraded answers. Health supports manual observations, plans, goals, and explicit meal proposal confirmation. The Recipes view supports structured editing, serving scaling, step-by-step cooking, planned meals, persistent grocery checkmarks, pasted schema.org JSON-LD previews and versioned JSON import/export. Nutrition values retain measured/estimated/reported provenance. They are not automatically imported into Health food logs. Unconfigured services return unavailable states; the UI does not fabricate readings, cluster health or demonstration data. Compute modes distinguish declared enrollment from verified online status.

## Fidelity

The palette, neutral colors, radius scale, buttons, cards, compact tables, and sidebar geometry are derived from shadcn/ui's neutral new-york family. The rendered DOM is produced by Rust/Dioxus; no React implementation is shipped. Visual equivalence depends on the same font metrics, CSS environment and component content. Compare browser screenshots with the matching upstream component reference before claiming pixel-level fidelity.

The original Moonfin client is launched externally; the console does not implement media playback. The console can replace the existing homepage only when the optional `services.homelabConsole` NixOS module is explicitly enabled and `replaceHomepage = true`; neither flag is enabled by default. The Nix module expects a prebuilt Dioxus web output containing `index.html` and assets, not an arbitrary Rust source directory.

See `THIRD_PARTY_NOTICES.md` for the upstream copyright license and `docs/console.md` at repository root for system interaction rules.


## Embedded Health MCP Apps

The `embedded-app` Cargo feature selects a Rust-hosted MCP Apps
entrypoint instead of the owner-session console. Five Dioxus Health views
share their domain UI with the standalone console but exchange data through
the MCP host's JSON-RPC `tools/call` surface; they receive no cookie,
bearer token or CSRF value. The transport validates the parent window,
correlates responses and limits outstanding calls.

Build source-only assets with:

```sh
ui/console/build-mcp-apps.sh /absolute/unused/output-directory
```

This command does not activate any service. It produces five resource HTML
entrypoints with a shared WASM bundle and a SHA-256 manifest. A trusted
static HTTPS origin must serve the bundle to the iframe, and the Health
service must be explicitly configured with the origin and verified bundle
path. See `docs/ui-resource-inventory.md` and
`services/health/internal/mcp/app_bundle.go` for the opt-in
source contracts. The legacy MCP Apps resources remain the default until
isolated WebAssembly, host and browser acceptance is completed.
