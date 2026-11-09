# User interface

`ui/console/` is the Rust/Dioxus browser client. The existing `nixosModules/console.nix` configuration serves its immutable compiled assets through an independent Nginx listener. The browser accesses only the private owner-session and permitted native resource routes; a public API listener and the unified MCP are not required.

The service runtime still owns authentication state, business rules and persistence. The UI serves neither privileged worker credentials nor a second domain database. It remains independently startable and replaceable without changing application state.

The browser and its same-origin private routes share the configured owner-session cookie and CSRF policy. Public API bearer credentials and MCP bearer credentials must not be exposed to the browser.
