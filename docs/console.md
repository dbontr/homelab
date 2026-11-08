# Unified Console

The console is a single client for the personal computing platform. It does not own an independent backend or duplicate the platform's source-of-truth data.

## Implementation

- Use Rust and Dioxus for browser-based and desktop clients. The initial target is web/WASM, with desktop packaging as a later build target.
- Use semantic HTML and CSS as the rendering interface. WebAssembly replaces application-side JavaScript logic, not the browser's document and style systems.
- The Go platform owns data, integration adapters, authentication, authorization, background work and persistence.
- The browser client consumes versioned same-origin native HTTP resources through the Go gateway. Its session is authenticated by a separate passphrase verifier and uses a short-lived, HttpOnly, Secure, SameSite=Strict cookie. Browser authorization is read-only and cannot invoke MCP commands. MCP is an automation interface; it is not a substitute for narrow, typed UI resource endpoints.
- shadcn/ui is a visual and interaction reference. Reimplement the necessary components and interaction patterns in Rust rather than translating React hooks or importing a React runtime.
- Start with established accessible controls. Dialogs, comboboxes, focus traps and navigation must be keyboard-operable, with correct native roles and focus behavior before customization.
- Keep Rust source components owned by this repository. Experimental third-party Rust component registries are references, not hard dependencies.

## User experience

A persistent shell provides navigation to Overview, Projects, Compute, Research, Learning, Health, Productivity, Media and Administration. These are views into one connected system, not separately styled applications.

Components use a small semantic vocabulary: button, field, card, table, navigation, tabs, dialog, alert, status and command palette. Build complex screens by composition instead of page-specific component variants.

Visual conventions:
- Neutral backgrounds, restrained borders and typography-led hierarchy; avoid gradients, decorative metrics and invented status indicators.
- Shared semantic tokens: background, foreground, surface, muted, accent, border, focus, danger, success and warning.
- Consistent spacing scale, compact data tables, distinguishable links and visible keyboard focus.
- Empty, loading, unavailable, permission-denied, incomplete, and stale-data states are first-class.
- Display only measurements and status returned by the API. Never synthesize "healthy", "online", or numeric readings for an unconnected service.
- Advanced actions identify the target, source and consequences before confirmation. Approval is completed through a trusted owner channel and is never a plain text chat prompt.

## Data and trust boundaries

The browser must not receive the platform's long-lived MCP bearer secret, OAuth refresh tokens, hardware SSH keys or privileged executor grants. It should authenticate to a same-origin Go session endpoint using an HttpOnly, Secure and SameSite cookie, with a CSRF defense for mutations. Session grants are limited to a user, origin, role and expiry. The read-only session layer exposes `GET /ui/session`, `POST /ui/session`, and CSRF-protected `DELETE /ui/session`. Its credentials and route scope are distinct from the master MCP token. The session remains bound to the running gateway process; process restarts invalidate sessions.

Read-only views can use the existing native endpoints for node/device inventory, compute profiles, tasks, goals, media requests, software and capability discovery. Extend the API with typed resources for cross-module project identities, compute job events and personal data only when those contracts have canonical owners.

Health information is sensitive and must remain behind the same authorization and session layer. Third-party or user-provided text, markdown, HTML and file previews must be rendered safely without interpreting arbitrary scripts, event handlers, or active document content.

The Go service is the only code that may communicate with the independent privileged executor. The Rust client requests a narrowly scoped operation; the executor verifies the owner approval, operation, target, lease and hardware identity before carrying out the action.

## Development sequence

1. Maintain the shadcn/ui new-york neutral design tokens and Rust component primitives as the canonical visual system.
2. Expose read-only owner browser sessions with strict origin, cookie and route enforcement.
3. Render inventory, project records, Workspace worker reports, tasks, profiles and software catalogs from the Go API.
4. Add scoped, CSRF-protected editing for project configuration, build profiles and the software library through the Go API.
5. Connect domain-specific Health, Learning and Productivity views to the canonical resource identifiers.
6. Integrate independently authorized advanced operations and desktop packaging after their backend contracts exist.

Component appearance is not authorization. All enforcement takes place in the Go backend and the privileged executor.

## Development source

The Rust/Dioxus implementation lives in `system/console`. It reproduces the neutral shadcn/ui palette and measurements in CSS, with no React runtime. The view hierarchy renders actual API responses and distinguishes missing service data from verified online state. The root Go gateway reads a separate `consolePassphraseFile` option through NixOS protected credentials when configured.

The design-system component set currently covers the console shell, sidebar navigation, cards, buttons, fields, tables, badges, tabs, notices and theme switcher. It is not a complete Rust port of the upstream registry. Pixel-level matching for a named shadcn component requires comparison against the corresponding upstream browser rendering.
