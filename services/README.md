# Services

Services own application state, business decisions, authentication, scheduled work, hardware operations, and long-running tasks. They do not depend on a browser, an MCP connection, or a public API listener.

The primary domain implementations are in `system/` while their deployment boundaries are organized by service. The separate Compute, Learning, and Health Go modules retain their own data and failure domains. Moonfin/Seerr, monitoring collectors, SQL-backed applications, and K3s remain independently operated infrastructure components; an interface outage must not stop them.

`services/bridge/` contains the restricted internal HTTP transport used by the optional MCP and public API entry points. It does not own application records or privileged operations. The service runtime remains the only owner of its writable state.

The browser uses owner-session routes and CSRF checks. Neither the browser nor an interface gateway may receive a service's administrator credential. Public interfaces require separate client and route-scoped service credentials; disable or stop those processes without removing the underlying services.
