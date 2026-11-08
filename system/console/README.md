# Homelab Console

The console is a Rust/WASM client for the Go personal computing platform. It uses standard HTML and CSS, shadcn/ui neutral design tokens, and native Dioxus components. No React runtime is required.

## Development

From `system/console`:

```sh
dx serve --platform web
```

Requires a Rust toolchain with the `wasm32-unknown-unknown` target, Dioxus CLI 0.7, and cached Cargo dependencies. Use a local reverse proxy to serve the compiled console and Go API under one HTTPS origin. Dioxus is a web application; the API must be reachable under the same origin.

Do not expose the gateway's MCP bearer secret to the browser. The browser uses a short-lived, read-only cookie authenticated through `POST /ui/session`. Go accepts GET access to selected native resources for that cookie and requires independent authorization for mutations.

The Go service must have `HOMELAB_CONSOLE_PASSPHRASE_FILE` pointing to an owner-only JSON passphrase verifier, provisioned independently of the administrator approval passphrase. Configure `HOMELAB_ALLOWED_ORIGINS` for the console's exact HTTPS origin. No browser session is available if the verifier is not configured.

## Views

Overview, Projects, Compute, Software Library, Tasks, Learning, Health, Media and Administration consume live API responses. Unconfigured services return unavailable states; the UI does not fabricate readings, cluster health or demonstration data. Compute modes distinguish declared enrollment from verified online status.

## Fidelity

The palette, neutral colors, radius scale, buttons, cards, compact tables, and sidebar geometry are derived from shadcn/ui's neutral new-york family. The rendered DOM is produced by Rust/Dioxus; no React implementation is shipped. Visual equivalence depends on the same font metrics, CSS environment and component content. Compare browser screenshots with the matching upstream component reference before claiming pixel-level fidelity.

See `THIRD_PARTY_NOTICES.md` for the upstream copyright license and `docs/console.md` at repository root for system interaction rules.
