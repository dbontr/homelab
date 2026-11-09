# External native API

`homelab-api` is an optional, separately executable external HTTP API. It passes only the native `/v1/` resource routes to the service runtime. It never forwards `/ui/`, `/mcp`, or tool-invocation routes.

The API is read-only by default. Each allowed write resource prefix must be declared in `services.homelabInterfaces.api.writePaths`, with exact path-segment matching. The domain runtime independently enforces the API credential's permitted route and write prefixes. The service continues to enforce domain validation and ownership; the API's allowlist is an additional constraint, not an authorization substitute.

The client bearer token and route-scoped service credential are distinct from each other and from the platform administrator token. The listener is disabled unless `services.homelabInterfaces.api.enable` is explicitly set. Its external bearer token must be distinct from the MCP token and the service-runtime bearer token. All listeners use numeric loopback addresses behind separately configured authenticated ingress.

Disabling this interface does not disable the web UI, the unified MCP, or underlying services.
